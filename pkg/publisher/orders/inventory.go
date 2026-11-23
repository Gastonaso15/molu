package orders

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
)

const consumableTypesEntity = "consumable_types"

// Named is a referenced record with its display name.
type Named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Warehouse is a physical or logical store of consumables. Mobile ones are the
// temporary warehouses a work order opens for its supplies.
type Warehouse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Location    string  `json:"location,omitempty"` // "lat,lng"
	Enabled     bool    `json:"enabled"`
	Mobile      bool    `json:"mobile,omitempty"`
	WorkOrderID string  `json:"work_order_id,omitempty"` // the order a mobile warehouse belongs to
	Responsible *Person `json:"responsible,omitempty"`
}

// WarehouseDetail is GetWarehouse's result: the warehouse and what it holds.
type WarehouseDetail struct {
	Warehouse
	Stock []StockLine `json:"stock"`
}

// ListWarehousesInput filters work_orders.ListWarehouses (AND).
type ListWarehousesInput struct {
	Name    string `json:"name,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
	Mobile  *bool  `json:"mobile,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// WarehouseList is the result of ListWarehouses, sorted by name.
type WarehouseList struct {
	Warehouses []Warehouse `json:"warehouses"`
	Total      int         `json:"total"`
	Truncated  bool        `json:"truncated,omitempty"`
}

// GetWarehouseInput is the payload of work_orders.GetWarehouse.
type GetWarehouseInput struct {
	WarehouseID ID `json:"warehouse_id"`
}

// Consumable is a supply item. StockMin is its alert threshold; TotalStock adds
// what every warehouse holds; LowStock says some warehouse holds StockMin or less.
type Consumable struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Type        *Named  `json:"type,omitempty"`
	Unit        *Named  `json:"unit,omitempty"`
	StockMin    float64 `json:"stock_min"`
	Enabled     bool    `json:"enabled"`
	TotalStock  float64 `json:"total_stock"`
	LowStock    bool    `json:"low_stock"`
}

// WarehouseStock is the quantity of one consumable in one warehouse.
type WarehouseStock struct {
	WarehouseID   string  `json:"warehouse_id"`
	WarehouseName string  `json:"warehouse_name"`
	Mobile        bool    `json:"mobile,omitempty"`
	Quantity      float64 `json:"quantity"`
}

// ConsumableDetail is GetConsumable's result: the consumable and where it is.
type ConsumableDetail struct {
	Consumable
	Stock []WarehouseStock `json:"stock"`
}

// ListConsumablesInput filters work_orders.ListConsumables (AND).
type ListConsumablesInput struct {
	Name             string `json:"name,omitempty"`
	Enabled          *bool  `json:"enabled,omitempty"`
	ConsumableTypeID ID     `json:"consumable_type_id,omitempty"`
	LowStock         *bool  `json:"low_stock,omitempty"`
	Limit            int    `json:"limit,omitempty"`
}

// ConsumableList is the result of ListConsumables, sorted by name.
type ConsumableList struct {
	Consumables []Consumable `json:"consumables"`
	Total       int          `json:"total"`
	Truncated   bool         `json:"truncated,omitempty"`
}

// GetConsumableInput is the payload of work_orders.GetConsumable.
type GetConsumableInput struct {
	ConsumableID ID `json:"consumable_id"`
}

// StockAlert says one warehouse holds a consumable at or below its minimum.
type StockAlert struct {
	ConsumableID    string  `json:"consumable_id"`
	ConsumableName  string  `json:"consumable_name"`
	Unit            string  `json:"unit,omitempty"`
	WarehouseID     string  `json:"warehouse_id"`
	WarehouseName   string  `json:"warehouse_name"`
	WarehouseMobile bool    `json:"warehouse_mobile,omitempty"`
	Quantity        float64 `json:"quantity"`
	StockMin        float64 `json:"stock_min"`
	Shortfall       float64 `json:"shortfall"` // stock_min - quantity
}

// ListLowStockInput filters work_orders.ListLowStock.
type ListLowStockInput struct {
	WarehouseID  ID  `json:"warehouse_id,omitempty"`
	ConsumableID ID  `json:"consumable_id,omitempty"`
	Limit        int `json:"limit,omitempty"`
}

// LowStockList is the result of ListLowStock, largest shortfall first.
type LowStockList struct {
	Alerts    []StockAlert `json:"alerts"`
	Total     int          `json:"total"`
	Truncated bool         `json:"truncated,omitempty"`
}

func warehouseFromDoc(id string, doc map[string]any) Warehouse {
	w := Warehouse{ID: id}
	w.Name, _ = doc["name"].(string)
	w.Description, _ = doc["description"].(string)
	w.Location, _ = doc["location"].(string)
	w.Enabled, _ = doc["enabled"].(bool)
	w.Mobile = doc["type"] == "mobile"
	if w.Mobile {
		if wo := idString(doc["work_order_id"]); wo != "" && wo != "0" {
			w.WorkOrderID = wo
		}
	}
	w.Responsible = personFrom(doc["responsible_user_id"])
	return w
}

func byName(n1, n2 string, id1, id2 string) bool {
	if n1 != n2 {
		return n1 < n2
	}
	return intOf(id1) < intOf(id2)
}

// ListWarehouses lists the warehouses sorted by name. Mobile warehouses (one
// per order that uses consumables) are included unless mobile=false.
func (s *Service) ListWarehouses(ctx context.Context, in ListWarehousesInput) (WarehouseList, error) {
	limit, problems := validLimit(in.Limit)
	if err := validationFrom(problems); err != nil {
		return WarehouseList{}, err
	}
	rows, truncated, err := s.scanCatalog(ctx, warehouseEntity)
	if err != nil {
		return WarehouseList{}, err
	}
	var list []Warehouse
	for _, r := range rows {
		w := warehouseFromDoc(idString(r["id"]), r)
		switch {
		case in.Name != "" && !containsFold(w.Name, in.Name):
		case in.Enabled != nil && w.Enabled != *in.Enabled:
		case in.Mobile != nil && w.Mobile != *in.Mobile:
		default:
			list = append(list, w)
		}
	}
	sort.Slice(list, func(i, j int) bool { return byName(list[i].Name, list[j].Name, list[i].ID, list[j].ID) })
	out := WarehouseList{Warehouses: []Warehouse{}, Truncated: truncated}
	if len(list) > limit {
		list, out.Truncated = list[:limit], true
	}
	out.Warehouses = append(out.Warehouses, list...)
	out.Total = len(out.Warehouses)
	return out, nil
}

// GetWarehouse returns one warehouse with the name of its responsible user and
// the consumables it holds.
func (s *Service) GetWarehouse(ctx context.Context, in GetWarehouseInput) (WarehouseDetail, error) {
	if !positiveID(in.WarehouseID) {
		return WarehouseDetail{}, &ValidationError{Fields: []string{"warehouse_id"}, Reason: "must be a positive id"}
	}
	id := strings.TrimSpace(string(in.WarehouseID))
	doc, err := s.sub.GetEntity(ctx, warehouseEntity, id)
	if err != nil {
		if isMissing(err) {
			return WarehouseDetail{}, &NotFoundError{ID: id, Kind: "warehouse"}
		}
		return WarehouseDetail{}, classify(err, "")
	}
	d := WarehouseDetail{Warehouse: warehouseFromDoc(id, doc)}
	if d.Responsible != nil {
		d.Responsible.Name = s.userNames(ctx)(d.Responsible.UserID)
	}
	if d.Stock, err = s.warehouseStock(ctx, id, doc); err != nil {
		return WarehouseDetail{}, err
	}
	return d, nil
}

// inventoryView is a snapshot of every consumable with what each warehouse
// holds, computed the way Seam does (see warehouseStock).
type inventoryView struct {
	consumables []map[string]any
	warehouses  map[string]Warehouse
	// stock maps consumable id -> warehouse id -> hundredths of a unit.
	stock     map[string]map[string]int64
	legacy    map[string]map[string]int64 // warehouse id -> consumable id -> hundredths
	truncated bool
}

// addConsumable computes the per-warehouse stock of one consumable document.
func (v *inventoryView) addConsumable(c map[string]any) error {
	cid := idString(c["id"])
	per := map[string]int64{}
	if raw, migrated := c["stock_by_warehouse"]; migrated && raw != nil {
		var byWarehouse map[string]any
		if err := roundTrip(raw, &byWarehouse); err != nil {
			return err
		}
		for wid, n := range byWarehouse {
			per[wid] = intOf(n)
		}
	} else {
		for wid, balances := range v.legacy {
			if n, ok := balances[cid]; ok {
				per[wid] = n
			}
		}
	}
	v.stock[cid] = per
	return nil
}

func (s *Service) loadInventory(ctx context.Context) (inventoryView, error) {
	v := inventoryView{warehouses: map[string]Warehouse{}, stock: map[string]map[string]int64{}}
	cons, t1, err := s.scanCatalog(ctx, consumableEntity)
	if err != nil {
		return v, err
	}
	whs, t2, err := s.scanCatalog(ctx, warehouseEntity)
	if err != nil {
		return v, err
	}
	v.consumables, v.truncated = cons, t1 || t2

	v.legacy = map[string]map[string]int64{}
	for _, w := range whs {
		wid := idString(w["id"])
		v.warehouses[wid] = warehouseFromDoc(wid, w)
		var balances []struct {
			ConsumableID any     `json:"consumable_id"`
			Stock        float64 `json:"stock"`
		}
		if err := roundTrip(w["consumables"], &balances); err != nil {
			return v, err
		}
		for _, b := range balances {
			if cid := idString(b.ConsumableID); cid != "" && cid != "0" {
				if v.legacy[wid] == nil {
					v.legacy[wid] = map[string]int64{}
				}
				v.legacy[wid][cid] = int64(math.Round(b.Stock * 100))
			}
		}
	}
	for _, c := range cons {
		if err := v.addConsumable(c); err != nil {
			return v, err
		}
	}
	return v, nil
}

// labelsOf maps id -> display label for a small reference entity, preferring
// the symbol field when there is one.
func (s *Service) labelsOf(ctx context.Context, entity string, preferSymbol bool) (map[string]string, error) {
	rows, _, err := s.scanCatalog(ctx, entity)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		label := ""
		if preferSymbol {
			label, _ = r["symbol"].(string)
		}
		if strings.TrimSpace(label) == "" {
			label, _ = r["name"].(string)
		}
		out[idString(r["id"])] = strings.TrimSpace(label)
	}
	return out, nil
}

func (v inventoryView) consumable(row map[string]any, types, units map[string]string) Consumable {
	id := idString(row["id"])
	c := Consumable{ID: id}
	c.Name, _ = row["name"].(string)
	c.Description, _ = row["description"].(string)
	c.Enabled, _ = row["enabled"].(bool)
	c.StockMin = floatOf(row["stock_min"])
	if tid := idString(row["consumable_type_id"]); tid != "" && tid != "0" {
		c.Type = &Named{ID: tid, Name: types[tid]}
	}
	if uid := idString(row["consumable_unit"]); uid != "" && uid != "0" {
		name := units[uid]
		if name == "" {
			name = "#" + uid
		}
		c.Unit = &Named{ID: uid, Name: name}
	}
	var total int64
	for _, n := range v.stock[id] {
		total += n
		if float64(n)/100 <= c.StockMin {
			c.LowStock = true
		}
	}
	c.TotalStock = float64(total) / 100
	return c
}

func floatOf(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	}
	f, _ := strconv.ParseFloat(idString(v), 64)
	return f
}

// ListConsumables lists the consumables sorted by name, with their type, unit,
// total stock and whether any warehouse is at or below the minimum.
func (s *Service) ListConsumables(ctx context.Context, in ListConsumablesInput) (ConsumableList, error) {
	limit, problems := validLimit(in.Limit)
	problems = append(problems, optionalIDProblem("consumable_type_id", in.ConsumableTypeID)...)
	if err := validationFrom(problems); err != nil {
		return ConsumableList{}, err
	}
	v, err := s.loadInventory(ctx)
	if err != nil {
		return ConsumableList{}, err
	}
	types, err := s.labelsOf(ctx, consumableTypesEntity, false)
	if err != nil {
		return ConsumableList{}, err
	}
	units, err := s.labelsOf(ctx, unitEntity, true)
	if err != nil {
		return ConsumableList{}, err
	}
	var list []Consumable
	for _, r := range v.consumables {
		c := v.consumable(r, types, units)
		switch {
		case in.Name != "" && !containsFold(c.Name, in.Name):
		case in.Enabled != nil && c.Enabled != *in.Enabled:
		case in.ConsumableTypeID != "" && (c.Type == nil || c.Type.ID != string(in.ConsumableTypeID)):
		case in.LowStock != nil && c.LowStock != *in.LowStock:
		default:
			list = append(list, c)
		}
	}
	sort.Slice(list, func(i, j int) bool { return byName(list[i].Name, list[j].Name, list[i].ID, list[j].ID) })
	out := ConsumableList{Consumables: []Consumable{}, Truncated: v.truncated}
	if len(list) > limit {
		list, out.Truncated = list[:limit], true
	}
	out.Consumables = append(out.Consumables, list...)
	out.Total = len(out.Consumables)
	return out, nil
}

// GetConsumable returns one consumable with what each warehouse holds of it.
func (s *Service) GetConsumable(ctx context.Context, in GetConsumableInput) (ConsumableDetail, error) {
	if !positiveID(in.ConsumableID) {
		return ConsumableDetail{}, &ValidationError{Fields: []string{"consumable_id"}, Reason: "must be a positive id"}
	}
	id := strings.TrimSpace(string(in.ConsumableID))
	doc, err := s.sub.GetEntity(ctx, consumableEntity, id)
	if err != nil {
		if isMissing(err) {
			return ConsumableDetail{}, &NotFoundError{ID: id, Kind: "consumable"}
		}
		return ConsumableDetail{}, classify(err, "")
	}
	v, err := s.loadInventory(ctx)
	if err != nil {
		return ConsumableDetail{}, err
	}
	types, err := s.labelsOf(ctx, consumableTypesEntity, false)
	if err != nil {
		return ConsumableDetail{}, err
	}
	units, err := s.labelsOf(ctx, unitEntity, true)
	if err != nil {
		return ConsumableDetail{}, err
	}
	// The scan is capped: if it did not reach this consumable, compute its
	// stock from the document read above.
	row := withID(doc, id)
	if _, ok := v.stock[id]; !ok {
		if err := v.addConsumable(row); err != nil {
			return ConsumableDetail{}, err
		}
	}
	d := ConsumableDetail{Consumable: v.consumable(row, types, units), Stock: []WarehouseStock{}}
	for wid, n := range v.stock[id] {
		w := v.warehouses[wid]
		name := w.Name
		if name == "" {
			name = "#" + wid
		}
		d.Stock = append(d.Stock, WarehouseStock{WarehouseID: wid, WarehouseName: name, Mobile: w.Mobile, Quantity: float64(n) / 100})
	}
	sort.Slice(d.Stock, func(i, j int) bool {
		return byName(d.Stock[i].WarehouseName, d.Stock[j].WarehouseName, d.Stock[i].WarehouseID, d.Stock[j].WarehouseID)
	})
	return d, nil
}

// ListLowStock lists every (consumable, warehouse) pair whose quantity is at or
// below the consumable's minimum, the same rule Seam uses to flag a consumable.
// Largest shortfall first. Mobile warehouses are included and marked.
func (s *Service) ListLowStock(ctx context.Context, in ListLowStockInput) (LowStockList, error) {
	limit, problems := validLimit(in.Limit)
	problems = append(problems, optionalIDProblem("warehouse_id", in.WarehouseID)...)
	problems = append(problems, optionalIDProblem("consumable_id", in.ConsumableID)...)
	if err := validationFrom(problems); err != nil {
		return LowStockList{}, err
	}
	v, err := s.loadInventory(ctx)
	if err != nil {
		return LowStockList{}, err
	}
	units, err := s.labelsOf(ctx, unitEntity, true)
	if err != nil {
		return LowStockList{}, err
	}
	var alerts []StockAlert
	for _, r := range v.consumables {
		cid := idString(r["id"])
		if in.ConsumableID != "" && cid != string(in.ConsumableID) {
			continue
		}
		min := floatOf(r["stock_min"])
		for wid, n := range v.stock[cid] {
			if in.WarehouseID != "" && wid != string(in.WarehouseID) {
				continue
			}
			qty := float64(n) / 100
			if qty > min {
				continue
			}
			w := v.warehouses[wid]
			a := StockAlert{ConsumableID: cid, WarehouseID: wid, WarehouseName: w.Name, WarehouseMobile: w.Mobile,
				Quantity: qty, StockMin: min, Shortfall: math.Round((min-qty)*100) / 100}
			a.ConsumableName, _ = r["name"].(string)
			a.Unit = units[idString(r["consumable_unit"])]
			alerts = append(alerts, a)
		}
	}
	sort.Slice(alerts, func(i, j int) bool {
		a, b := alerts[i], alerts[j]
		switch {
		case a.Shortfall != b.Shortfall:
			return a.Shortfall > b.Shortfall
		case a.ConsumableName != b.ConsumableName:
			return a.ConsumableName < b.ConsumableName
		}
		return intOf(a.WarehouseID) < intOf(b.WarehouseID)
	})
	out := LowStockList{Alerts: []StockAlert{}, Truncated: v.truncated}
	if len(alerts) > limit {
		alerts, out.Truncated = alerts[:limit], true
	}
	out.Alerts = append(out.Alerts, alerts...)
	out.Total = len(out.Alerts)
	return out, nil
}
