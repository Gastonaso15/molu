package orders

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
)

const (
	workOrderBlocksEntity = "work_order_blocks"
	warehouseEntity       = "warehouse"
	unitEntity            = "unit"
	maxBlocksPerOrder     = 100
	maxConsumableScan     = 1000
)

// GetOrder returns the order in full, or NotFoundError. Besides the order
// document it reads what hangs from it: the blocks, the mobile warehouse with
// its stock, and (for orders driven by a xolu machine) the FSM history. An
// entity that does not exist yet reads as empty; any other failure to read
// those parts fails the call rather than returning a partial record.
func (s *Service) GetOrder(ctx context.Context, id string) (OrderDetail, error) {
	if strings.TrimSpace(id) == "" {
		return OrderDetail{}, &ValidationError{Fields: []string{"order_id"}}
	}
	doc, err := s.sub.GetEntity(ctx, EntityType, id)
	if err != nil {
		return OrderDetail{}, classify(err, id)
	}
	d := OrderDetail{WorkOrder: orderFromDoc(id, doc)}
	d.CreatedAt, _ = doc["created_at"].(string)
	d.UpdatedAt, _ = doc["updated_at"].(string)
	if u := idString(doc["claimed_by_user_id"]); u != "" && u != "0" {
		d.ClaimedByUserID = u
	}
	sig, _ := doc["technician_signature"].(string)
	d.Signed = strings.TrimSpace(sig) != ""
	if bv, ok := doc["block_values"].(map[string]any); ok && len(bv) > 0 {
		d.BlockValues = bv
	}

	if len(d.BlockValues) > 0 && templateOrder(doc) {
		tpl, err := s.loadTemplate(ctx, idString(doc["template_id"]))
		var ve *ValidationError
		switch {
		case err == nil:
			d.BlockAnswers = decodeBlockAnswers(tpl, d.BlockValues)
		case !errors.As(err, &ve): // a template that cannot be read leaves the raw values
			return OrderDetail{}, err
		}
	}
	if d.Blocks, err = s.orderBlocks(ctx, id); err != nil {
		return OrderDetail{}, err
	}
	if executionOrder(doc) {
		d.HeaderConfirmedAt, _ = doc["execution_header_completed_at"].(string)
		// Orders that run blocks keep each block's answers on its own record.
		merged := map[string]any{}
		for _, b := range d.Blocks {
			for k, v := range b.Values {
				merged[k] = v
			}
		}
		if len(merged) > 0 {
			tpl, terr := s.loadTemplate(ctx, idString(doc["template_id"]))
			var ve *ValidationError
			switch {
			case terr == nil:
				d.BlockAnswers = decodeBlockAnswers(tpl, merged)
			case !errors.As(terr, &ve):
				return OrderDetail{}, terr
			}
		}
	}
	if wid := idString(doc["mobile_warehouse_id"]); wid != "" && wid != "0" {
		if d.MobileWarehouse, err = s.mobileWarehouse(ctx, wid); err != nil {
			return OrderDetail{}, err
		}
	}
	if mid := idString(doc["machine_id"]); !templateOrder(doc) && mid != "" && mid != "0" {
		hist, err := s.sub.MachineHistory(ctx, mid)
		if err != nil && !isMissing(err) {
			return OrderDetail{}, classify(err, id)
		}
		d.History = hist
	}
	return d, nil
}

func (s *Service) orderBlocks(ctx context.Context, orderID string) ([]OrderBlock, error) {
	rows, err := s.sub.FindEntities(ctx, workOrderBlocksEntity, EntityQuery{
		Where: []Condition{{Field: "work_order_id", Op: "=", Value: numericOrString(orderID)}},
		Limit: maxBlocksPerOrder, OrderBy: []string{"id"},
	})
	if err != nil {
		if isMissing(err) {
			return nil, nil
		}
		return nil, classify(err, orderID)
	}
	var blocks []OrderBlock
	for _, r := range rows {
		b := OrderBlock{ID: idString(r["id"]), Index: int(intOf(r["block_index"]))}
		b.Name, _ = r["name"].(string)
		b.State, _ = r["state"].(string)
		b.ResponsibleType, _ = r["responsible_type"].(string)
		if rid := idString(r["responsible_id"]); rid != "" && rid != "0" {
			b.ResponsibleID = rid
		}
		b.StartedAt, _ = r["started_at"].(string)
		b.CompletedAt, _ = r["completed_at"].(string)
		if v, ok := r["values"].(map[string]any); ok && len(v) > 0 {
			b.Values = v
		}
		if p := idString(r["predecessor_index"]); p != "" && intOf(r["predecessor_index"]) >= 0 {
			n := int(intOf(r["predecessor_index"]))
			b.PredecessorIndex = &n
		}
		blocks = append(blocks, b)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Index < blocks[j].Index })
	return blocks, nil
}

// mobileWarehouse reads the warehouse an order opened. A warehouse that does
// not exist is omitted (nil), not an error.
func (s *Service) mobileWarehouse(ctx context.Context, id string) (*MobileWarehouse, error) {
	doc, err := s.sub.GetEntity(ctx, warehouseEntity, id)
	if err != nil {
		if isMissing(err) {
			return nil, nil
		}
		return nil, classify(err, id)
	}
	w := &MobileWarehouse{ID: id}
	w.Name, _ = doc["name"].(string)
	w.Enabled, _ = doc["enabled"].(bool)
	if u := idString(doc["responsible_user_id"]); u != "" && u != "0" {
		w.ResponsibleUserID = u
	}
	if w.Stock, err = s.warehouseStock(ctx, id, doc); err != nil {
		return nil, err
	}
	return w, nil
}

// warehouseStock computes what a warehouse holds the way Seam does. Stock lives
// on each consumable as stock_by_warehouse (warehouse id -> hundredths of a
// unit). A consumable that has no such map yet falls back to the balance kept
// on the warehouse document (consumables: [{consumable_id, stock}]); once it
// has one, only the map counts.
func (s *Service) warehouseStock(ctx context.Context, warehouseID string, wh map[string]any) ([]StockLine, error) {
	units := map[string]int64{} // consumable id -> hundredths
	var legacy []struct {
		ConsumableID any     `json:"consumable_id"`
		Stock        float64 `json:"stock"`
	}
	if err := roundTrip(wh["consumables"], &legacy); err != nil {
		return nil, err
	}
	for _, l := range legacy {
		if cid := idString(l.ConsumableID); cid != "" && cid != "0" {
			units[cid] = int64(math.Round(l.Stock * 100))
		}
	}

	rows, err := s.sub.FindEntities(ctx, consumableEntity, EntityQuery{Limit: maxConsumableScan, OrderBy: []string{"id"}})
	if err != nil && !isMissing(err) {
		return nil, classify(err, warehouseID)
	}
	type info struct {
		name   string
		unitID string
	}
	known := map[string]info{}
	for _, r := range rows {
		cid := idString(r["id"])
		in := info{unitID: idString(r["consumable_unit"])}
		in.name, _ = r["name"].(string)
		known[cid] = in
		raw, migrated := r["stock_by_warehouse"]
		if !migrated || raw == nil {
			continue
		}
		delete(units, cid)
		var byWarehouse map[string]any
		if err := roundTrip(raw, &byWarehouse); err != nil {
			return nil, err
		}
		if v, ok := byWarehouse[warehouseID]; ok {
			units[cid] = intOf(v)
		}
	}

	labels := map[string]string{}
	label := func(unitID string) string {
		if unitID == "" || unitID == "0" {
			return ""
		}
		if l, ok := labels[unitID]; ok {
			return l
		}
		var l string
		if u, err := s.sub.GetEntity(ctx, unitEntity, unitID); err == nil {
			l, _ = u["symbol"].(string)
			if strings.TrimSpace(l) == "" {
				l, _ = u["name"].(string)
			}
		}
		labels[unitID] = strings.TrimSpace(l)
		return labels[unitID]
	}

	lines := make([]StockLine, 0, len(units))
	for cid, n := range units {
		in := known[cid]
		name := in.name
		if strings.TrimSpace(name) == "" {
			name = "#" + cid
		}
		lines = append(lines, StockLine{ConsumableID: cid, Name: name, Unit: label(in.unitID), Quantity: float64(n) / 100})
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].Name != lines[j].Name {
			return lines[i].Name < lines[j].Name
		}
		return intOf(lines[i].ConsumableID) < intOf(lines[j].ConsumableID)
	})
	return lines, nil
}
