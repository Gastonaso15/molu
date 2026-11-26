package orders

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// consumptionsEntity holds, per order and consumable, what was loaded into the
// order's mobile warehouse (status used), what was skipped, and the final
// settlement (status settled).
const consumptionsEntity = "work_order_consumptions"

// Intents of RecordOrderConsumable.
const (
	ConsumeConfirm = "confirm" // load a planned consumable
	ConsumeAdd     = "add"     // load a consumable that is not planned (templates that allow extras)
	ConsumeMore    = "more"    // load more of one already loaded
	ConsumeSkip    = "skip"    // a planned consumable will not be used
)

// consumptionKey derives, as the work order module does, the key that
// identifies one consumption record of an order: it is also the request_id of
// the movement that goes with it, and its receipt id gives the record its id.
func consumptionKey(orderID, consumableID int64, request string) (string, int64) {
	identity := fmt.Sprintf("work_order:%d:consumable:%d", orderID, consumableID)
	if request != "" {
		identity += ":" + request
	}
	sum := sha256.Sum256([]byte(identity))
	key := fmt.Sprintf("%x", sum[:16])
	id, _ := movementReceiptID(key)
	return key, id
}

// consumableLine is one consumable of an order as its records add up.
type consumableLine struct {
	id        int64
	suggested float64
	loaded    int64  // hundredths transferred into the mobile warehouse
	status    string // "", used or skipped
	settled   bool
	used      int64
	returned  int64
	returnTo  int64
}

// OrderConsumable is what GetOrder and the consumption tools report for one
// consumable of an order. Quantities are in display units.
type OrderConsumable struct {
	ConsumableID string  `json:"consumable_id"`
	Name         string  `json:"name,omitempty"`
	Unit         string  `json:"unit,omitempty"`
	Suggested    float64 `json:"suggested,omitempty"` // the template's default quantity
	// Status is "" (nothing recorded yet), used (loaded into the mobile
	// warehouse) or skipped.
	Status string  `json:"status,omitempty"`
	Loaded float64 `json:"loaded"` // transferred into the mobile warehouse so far
	// Settled lines have their final Used and Returned quantities and the
	// warehouse the surplus went to.
	Settled           bool    `json:"settled"`
	Used              float64 `json:"used,omitempty"`
	Returned          float64 `json:"returned,omitempty"`
	ReturnWarehouseID string  `json:"return_warehouse_id,omitempty"`
}

// consumableLines adds up an order's planned consumables (the template's plus
// the extras chosen when the order was opened) and its consumption records.
func (s *Service) consumableLines(ctx context.Context, orderID int64, tpl workOrderTemplate, doc map[string]any, mobileID int64) ([]consumableLine, error) {
	var lines []consumableLine
	pos := map[int64]int{}
	for _, c := range tpl.Consumables {
		if id := intOf(c.ConsumableID); id > 0 {
			if _, seen := pos[id]; !seen {
				pos[id] = len(lines)
				lines = append(lines, consumableLine{id: id, suggested: c.Quantity})
			}
		}
	}
	for _, e := range idList(doc["extra_consumable_ids"]) {
		if id := intOf(e); id > 0 {
			if _, seen := pos[id]; !seen {
				pos[id] = len(lines)
				lines = append(lines, consumableLine{id: id})
			}
		}
	}
	rows, err := s.sub.FindEntities(ctx, consumptionsEntity, EntityQuery{
		Where: []Condition{{Field: "work_order_id", Op: "=", Value: orderID}},
		Limit: 500, OrderBy: []string{"id"},
	})
	if err != nil && !isMissing(err) {
		return nil, classify(err, strconv.FormatInt(orderID, 10))
	}
	for _, r := range rows {
		id := intOf(r["consumable_id"])
		i, ok := pos[id]
		if !ok {
			i = len(lines)
			pos[id] = i
			lines = append(lines, consumableLine{id: id})
		}
		l := &lines[i]
		switch r["status"] {
		case "used":
			units, err := movementStoredUnits(floatOf(r["quantity"]))
			if err != nil {
				return nil, err
			}
			l.status = "used"
			if intOf(r["origin_warehouse_id"]) > 0 && intOf(r["target_warehouse_id"]) == mobileID {
				l.loaded += units
			}
		case "settled":
			used, err := movementStoredUnits(floatOf(r["quantity"]))
			if err != nil {
				return nil, err
			}
			returned, err := movementStoredUnits(floatOf(r["returned_quantity"]))
			if err != nil {
				return nil, err
			}
			l.settled, l.used, l.returned, l.returnTo = true, used, returned, intOf(r["target_warehouse_id"])
		case "skipped":
			if l.status == "" {
				l.status = "skipped"
			}
		}
	}
	return lines, nil
}

// consumableViews names and measures the lines for the report.
func (s *Service) consumableViews(ctx context.Context, lines []consumableLine) ([]OrderConsumable, error) {
	units := map[string]string{}
	out := make([]OrderConsumable, 0, len(lines))
	for _, l := range lines {
		v := OrderConsumable{
			ConsumableID: strconv.FormatInt(l.id, 10), Suggested: l.suggested, Status: l.status,
			Loaded: float64(l.loaded) / 100, Settled: l.settled,
		}
		if l.settled {
			v.Used, v.Returned = float64(l.used)/100, float64(l.returned)/100
			if l.returnTo > 0 {
				v.ReturnWarehouseID = strconv.FormatInt(l.returnTo, 10)
			}
		}
		doc, err := s.sub.GetEntity(ctx, consumableEntity, v.ConsumableID)
		switch {
		case err == nil:
			v.Name, _ = doc["name"].(string)
			if uid := idString(doc["consumable_unit"]); uid != "" && uid != "0" {
				sym, known := units[uid]
				if !known {
					if u, uerr := s.sub.GetEntity(ctx, unitEntity, uid); uerr == nil {
						sym, _ = u["symbol"].(string)
					}
					units[uid] = sym
				}
				v.Unit = sym
			}
		case !isMissing(err):
			return nil, classify(err, "")
		}
		out = append(out, v)
	}
	return out, nil
}

func findLine(lines []consumableLine, id int64) *consumableLine {
	for i := range lines {
		if lines[i].id == id {
			return &lines[i]
		}
	}
	return nil
}

// consumptionContext checks what both consumption tools need: an order that
// runs under the execution model, from a template with consumables, with a
// mobile warehouse, and an actor who is the order's responsible.
func (s *Service) consumptionContext(ctx context.Context, orderID string, userID int64) (*execution, int64, error) {
	x, err := s.loadExecution(ctx, orderID)
	if err != nil {
		return nil, 0, err
	}
	if !x.tpl.UsesConsumables {
		return nil, 0, rejection("consumables_disabled", "this order's template does not enable consumables", x.state)
	}
	if err := s.requireOwner(ctx, x, userID); err != nil {
		return nil, 0, err
	}
	mobile := intOf(x.doc["mobile_warehouse_id"])
	if mobile <= 0 {
		return nil, 0, rejection("warehouse_missing", "the order has no mobile warehouse", x.state)
	}
	return x, mobile, nil
}

// RecordConsumableInput is the payload of work_orders.RecordOrderConsumable.
type RecordConsumableInput struct {
	OrderID      string `json:"order_id"`
	ConsumableID ID     `json:"consumable_id"`
	// UserID is the user who records; it must be the order's responsible.
	UserID ID `json:"user_id"`
	// Intent is confirm, add, more or skip.
	Intent string `json:"intent"`
	// Quantity (confirm, add, more) is what is taken from the origin
	// warehouse into the order's mobile warehouse, at most two decimals.
	Quantity          *float64 `json:"quantity,omitempty"`
	OriginWarehouseID ID       `json:"origin_warehouse_id,omitempty"`
	// RequestID (more) identifies this extra load so it can be retried
	// safely; one is generated when omitted and returned.
	RequestID string `json:"request_id,omitempty"`
}

// RecordConsumableResult reports the consumable after the intent.
type RecordConsumableResult struct {
	Consumable OrderConsumable `json:"consumable"`
	// Movement is the transfer into the mobile warehouse (not for skip).
	Movement        *MovementResult `json:"movement,omitempty"`
	RequestID       string          `json:"request_id,omitempty"`
	AlreadyRecorded bool            `json:"already_recorded,omitempty"`
}

// RecordOrderConsumable records what an order takes for its work, as the work
// order module does while the order runs: a planned consumable is confirmed
// (confirm) or skipped (skip), one that is not planned is added (add, only if
// the template allows extras) and a consumable already loaded can get more
// (more). Loading is a transfer from an origin warehouse into the order's
// mobile warehouse, saved in one Commit with the consumption record. It needs
// the header confirmed and the order open, and the actor must be the order's
// responsible. Repeating the same request changes nothing.
func (s *Service) RecordOrderConsumable(ctx context.Context, in RecordConsumableInput) (RecordConsumableResult, error) {
	userID, err := actingUser(in.UserID)
	if err != nil {
		return RecordConsumableResult{}, err
	}
	var problems []fieldProblem
	consID, ok := parsePositiveID(in.ConsumableID)
	if !ok {
		problems = append(problems, fieldProblem{"consumable_id", "must be a positive id"})
	}
	intent := strings.TrimSpace(in.Intent)
	switch intent {
	case ConsumeConfirm, ConsumeAdd, ConsumeMore, ConsumeSkip:
	default:
		problems = append(problems, fieldProblem{"intent", "must be confirm, add, more or skip"})
	}
	var units, originID int64
	if intent != ConsumeSkip && intent != "" {
		if in.Quantity == nil {
			problems = append(problems, fieldProblem{"quantity", "required"})
		} else if u, qerr := movementStoredUnits(*in.Quantity); qerr != nil || u == 0 {
			problems = append(problems, fieldProblem{"quantity", "must be a positive number with at most two decimal places"})
		} else {
			units = u
		}
		if originID, ok = parsePositiveID(in.OriginWarehouseID); !ok {
			problems = append(problems, fieldProblem{"origin_warehouse_id", "required: the warehouse the consumable is taken from"})
		}
	}
	request := strings.TrimSpace(in.RequestID)
	if request != "" {
		if _, valid := movementReceiptID(request); !valid {
			problems = append(problems, fieldProblem{"request_id", "must be 32 lowercase hexadecimal characters"})
		}
		if intent != ConsumeMore {
			problems = append(problems, fieldProblem{"request_id", "only used with intent more"})
		}
	}
	if err := validationFrom(problems); err != nil {
		return RecordConsumableResult{}, err
	}

	s.consumptionMu.Lock()
	defer s.consumptionMu.Unlock()

	x, mobile, err := s.consumptionContext(ctx, in.OrderID, userID)
	if err != nil {
		return RecordConsumableResult{}, err
	}
	if intent != ConsumeSkip && originID == mobile {
		return RecordConsumableResult{}, validationFrom([]fieldProblem{{"origin_warehouse_id", "must differ from the order's mobile warehouse"}})
	}
	if !x.open {
		return RecordConsumableResult{}, closedRejection(x)
	}
	if !x.headerReady {
		return RecordConsumableResult{}, rejection("header_not_confirmed", "confirm the execution header (ConfirmExecutionHeader) before recording consumables", x.state)
	}
	orderID := intOf(x.id)
	lines, err := s.consumableLines(ctx, orderID, x.tpl, x.doc, mobile)
	if err != nil {
		return RecordConsumableResult{}, err
	}
	planned := false
	for _, c := range x.tpl.Consumables {
		planned = planned || intOf(c.ConsumableID) == consID
	}
	for _, e := range idList(x.doc["extra_consumable_ids"]) {
		planned = planned || intOf(e) == consID
	}
	switch intent {
	case ConsumeAdd:
		if !x.tpl.ExtraConsumablesAllowed {
			return RecordConsumableResult{}, rejection("extras_not_allowed", "this template does not allow consumables that are not planned", x.state)
		}
		if planned || findLine(lines, consID) != nil {
			return RecordConsumableResult{}, rejection("already_listed", "the consumable is already on the order: use confirm or more", x.state)
		}
	case ConsumeConfirm, ConsumeSkip:
		if !planned {
			return RecordConsumableResult{}, validationFrom([]fieldProblem{{"consumable_id", "not planned for this order (use intent add where the template allows extras)"}})
		}
	case ConsumeMore:
		first := findLine(lines, consID)
		if first == nil || first.loaded == 0 {
			return RecordConsumableResult{}, rejection("not_recorded", "more adds to a consumable already loaded into the order", x.state)
		}
		if request == "" {
			if request, err = newMovementRequestID(); err != nil {
				return RecordConsumableResult{}, fmt.Errorf("generate request_id: %w", err)
			}
		}
	}

	suffix := ""
	if intent == ConsumeMore {
		suffix = request
	}
	key, recordID := consumptionKey(orderID, consID, suffix)
	status := "used"
	if intent == ConsumeSkip {
		status = "skipped"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	data := map[string]any{
		"work_order_id": orderID, "consumable_id": consID, "quantity": float64(units) / 100,
		"status": status, "user_id": userID, "created_at": now,
	}
	if status == "used" {
		data["origin_warehouse_id"], data["target_warehouse_id"] = originID, mobile
	}
	view := func(movement *MovementResult, already bool) (RecordConsumableResult, error) {
		fresh, err := s.consumableLines(ctx, orderID, x.tpl, x.doc, mobile)
		if err != nil {
			return RecordConsumableResult{}, err
		}
		views, err := s.consumableViews(ctx, []consumableLine{*findLine(fresh, consID)})
		if err != nil {
			return RecordConsumableResult{}, err
		}
		res := RecordConsumableResult{Consumable: views[0], Movement: movement, AlreadyRecorded: already}
		if intent == ConsumeMore {
			res.RequestID = request
		}
		return res, nil
	}
	// A record saved by an earlier attempt of this same request is a success;
	// one saved with other content is a conflict.
	existing := func() (exists, matches bool, err error) {
		saved, gerr := s.sub.GetEntity(ctx, consumptionsEntity, strconv.FormatInt(recordID, 10))
		if gerr != nil {
			if isMissing(gerr) {
				return false, false, nil
			}
			return false, false, classify(gerr, "")
		}
		matches = intOf(saved["work_order_id"]) == orderID && intOf(saved["consumable_id"]) == consID &&
			saved["status"] == status && floatOf(saved["quantity"]) == float64(units)/100
		if status == "used" {
			matches = matches && intOf(saved["origin_warehouse_id"]) == originID && intOf(saved["target_warehouse_id"]) == mobile
		}
		return true, matches, nil
	}
	conflict := rejection("already_recorded", "this consumable was already recorded for the order with different content", x.state)
	if ex, match, err := existing(); err != nil {
		return RecordConsumableResult{}, err
	} else if ex {
		if !match {
			return RecordConsumableResult{}, conflict
		}
		return view(nil, true)
	}

	record := CommitAppend{Entity: consumptionsEntity, ID: &recordID, Data: data}
	if status == "skipped" {
		version := int(intOf(x.doc["_version"]))
		if version <= 0 {
			return RecordConsumableResult{}, fmt.Errorf("work order %s has no _version: cannot change it safely", x.id)
		}
		order := make(map[string]any, len(x.doc))
		for k, v := range x.doc {
			if k != "_version" && k != "id" {
				order[k] = v
			}
		}
		order["updated_at"] = now
		err := s.sub.Commit(ctx, CommitRequest{
			Update: CommitUpdate{Entity: EntityType, ID: orderID, Version: &version, Data: order},
			Append: []CommitAppend{record},
		})
		if err != nil {
			if ex, match, _ := existing(); ex && match {
				return view(nil, true)
			}
			if r := staleOrConflict(err, x.state, "the order"); r != nil {
				return RecordConsumableResult{}, r
			}
			return RecordConsumableResult{}, classify(err, x.id)
		}
		return view(nil, false)
	}

	reference := "WO #" + x.id
	mv, err := s.registerMovement(ctx, MovementInput{
		Type: MovementTransfer, ConsumableID: ID(strconv.FormatInt(consID, 10)), UserID: ID(strconv.FormatInt(userID, 10)),
		OriginWarehouseID: ID(strconv.FormatInt(originID, 10)), TargetWarehouseID: ID(strconv.FormatInt(mobile, 10)),
		Quantity: floatPtr(float64(units) / 100), Reason: reference, Reference: reference, RequestID: key,
	}, record)
	if err != nil {
		if ex, match, _ := existing(); ex && match {
			return view(nil, true)
		}
		return RecordConsumableResult{}, err
	}
	return view(&mv, mv.AlreadyProcessed)
}

// SettleConsumableInput is the payload of work_orders.SettleOrderConsumable.
type SettleConsumableInput struct {
	OrderID      string `json:"order_id"`
	ConsumableID ID     `json:"consumable_id"`
	// UserID is the user who settles; it must be the order's responsible.
	UserID ID `json:"user_id"`
	// Used is how much of what was loaded was actually used (0 up to the
	// loaded quantity, at most two decimals).
	Used *float64 `json:"used"`
	// ReturnWarehouseID receives the surplus; required when Used is less
	// than what was loaded.
	ReturnWarehouseID ID `json:"return_warehouse_id,omitempty"`
}

// SettleConsumableResult reports the consumable after its settlement.
type SettleConsumableResult struct {
	Consumable     OrderConsumable `json:"consumable"`
	AlreadySettled bool            `json:"already_settled,omitempty"`
}

// SettleOrderConsumable closes one consumable of a finished order, as the work
// order module does once the order reaches its final state: what was used
// leaves the mobile warehouse (an exit) and the surplus goes to a return
// warehouse (a transfer), in one Commit with the settlement record. A
// consumable can be settled once; repeating the same settlement changes
// nothing.
func (s *Service) SettleOrderConsumable(ctx context.Context, in SettleConsumableInput) (SettleConsumableResult, error) {
	userID, err := actingUser(in.UserID)
	if err != nil {
		return SettleConsumableResult{}, err
	}
	var problems []fieldProblem
	consID, ok := parsePositiveID(in.ConsumableID)
	if !ok {
		problems = append(problems, fieldProblem{"consumable_id", "must be a positive id"})
	}
	var used int64
	if in.Used == nil {
		problems = append(problems, fieldProblem{"used", "required (0 when nothing was used)"})
	} else if u, uerr := movementStoredUnits(*in.Used); uerr != nil {
		problems = append(problems, fieldProblem{"used", "must be a non-negative number with at most two decimal places"})
	} else {
		used = u
	}
	var returnID int64
	if strings.TrimSpace(string(in.ReturnWarehouseID)) != "" {
		if returnID, ok = parsePositiveID(in.ReturnWarehouseID); !ok {
			problems = append(problems, fieldProblem{"return_warehouse_id", "must be a positive id"})
		}
	}
	if err := validationFrom(problems); err != nil {
		return SettleConsumableResult{}, err
	}

	s.consumptionMu.Lock()
	defer s.consumptionMu.Unlock()

	x, mobile, err := s.consumptionContext(ctx, in.OrderID, userID)
	if err != nil {
		return SettleConsumableResult{}, err
	}
	if x.idx != x.last {
		return SettleConsumableResult{}, rejection("not_finished", "consumables are settled once the order reaches its final state", x.state)
	}
	orderID := intOf(x.id)
	lines, err := s.consumableLines(ctx, orderID, x.tpl, x.doc, mobile)
	if err != nil {
		return SettleConsumableResult{}, err
	}
	line := findLine(lines, consID)
	if line == nil || line.loaded == 0 {
		return SettleConsumableResult{}, rejection("nothing_loaded", "nothing of this consumable was loaded into the order's mobile warehouse", x.state)
	}
	if used > line.loaded {
		return SettleConsumableResult{}, validationFrom([]fieldProblem{{"used", fmt.Sprintf("cannot exceed what was loaded (%g)", float64(line.loaded)/100)}})
	}
	surplus := line.loaded - used
	if surplus == 0 {
		returnID = 0
	} else if returnID == 0 || returnID == mobile {
		return SettleConsumableResult{}, validationFrom([]fieldProblem{{"return_warehouse_id", "required: a warehouse other than the mobile one to receive the surplus"}})
	}
	respond := func(already bool) (SettleConsumableResult, error) {
		fresh, err := s.consumableLines(ctx, orderID, x.tpl, x.doc, mobile)
		if err != nil {
			return SettleConsumableResult{}, err
		}
		views, err := s.consumableViews(ctx, []consumableLine{*findLine(fresh, consID)})
		if err != nil {
			return SettleConsumableResult{}, err
		}
		return SettleConsumableResult{Consumable: views[0], AlreadySettled: already}, nil
	}
	if line.settled {
		if line.used == used && line.returnTo == returnID {
			return respond(true)
		}
		return SettleConsumableResult{}, rejection("already_recorded", "this consumable was already settled with a different used quantity or return warehouse", x.state)
	}

	if err := s.saveSettlement(ctx, x, line, mobile, used, returnID, userID); err != nil {
		// A settlement saved by an earlier attempt is a success.
		if fresh, ferr := s.consumableLines(ctx, orderID, x.tpl, x.doc, mobile); ferr == nil {
			if l := findLine(fresh, consID); l != nil && l.settled && l.used == used && l.returnTo == returnID {
				return respond(true)
			}
		}
		return SettleConsumableResult{}, err
	}
	return respond(false)
}

func (s *Service) saveSettlement(ctx context.Context, x *execution, line *consumableLine, mobile, used, returnID, userID int64) error {
	s.movementMu.Lock()
	defer s.movementMu.Unlock()

	orderID := intOf(x.id)
	consIDStr := strconv.FormatInt(line.id, 10)
	doc, err := s.sub.GetEntity(ctx, consumableEntity, consIDStr)
	if err != nil {
		if isMissing(err) {
			return validationFrom([]fieldProblem{{"consumable_id", fmt.Sprintf("consumable %d does not exist", line.id)}})
		}
		return classify(err, "")
	}
	version := int(intOf(doc["_version"]))
	if version <= 0 {
		return fmt.Errorf("consumable %d has no valid _version: cannot update stock safely", line.id)
	}
	stock, err := s.loadConsumableStockMap(ctx, line.id, doc)
	if err != nil {
		return err
	}
	balance, err := s.checkMovementWarehouse(ctx, mobile, stock, "mobile_warehouse_id")
	if err != nil {
		return err
	}
	if balance < line.loaded {
		return rejection("insufficient_stock", "the mobile warehouse holds less than was loaded", x.state)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	reference := "WO #" + x.id
	req := movementFingerprintPayload{ResponsibleUserID: userID, OriginWarehouseID: mobile, ConsumableID: line.id, Reason: reference, Reference: reference}
	var appends []CommitAppend
	if used > 0 {
		req.Type, req.Quantity = MovementExit, used
		req.RequestID, _ = consumptionKey(orderID, line.id, "settlement-exit")
		appends = append(appends, CommitAppend{Entity: movementEntity, Data: movementLedgerData(req, balance, balance-used, 0, 0, now)})
		balance -= used
	}
	surplus := line.loaded - used
	if surplus > 0 {
		targetStock, err := s.checkMovementWarehouse(ctx, returnID, stock, "return_warehouse_id")
		if err != nil {
			return err
		}
		after := targetStock + surplus
		if after > movementMaxUnits {
			return rejection("stock_overflow", "resulting stock is outside the allowed range", x.state)
		}
		req.Type, req.Quantity, req.TargetWarehouseID = MovementTransfer, surplus, returnID
		req.RequestID, _ = consumptionKey(orderID, line.id, fmt.Sprintf("settlement-return:%d", returnID))
		appends = append(appends, CommitAppend{Entity: movementEntity, Data: movementLedgerData(req, balance, balance-surplus, targetStock, after, now)})
		balance -= surplus
		stock[returnID] = after
	}
	stock[mobile] = balance
	_, settledID := consumptionKey(orderID, line.id, "settled")
	appends = append(appends, CommitAppend{Entity: consumptionsEntity, ID: &settledID, Data: map[string]any{
		"work_order_id": orderID, "consumable_id": line.id, "status": "settled",
		"quantity": float64(used) / 100, "returned_quantity": float64(surplus) / 100,
		"origin_warehouse_id": mobile, "target_warehouse_id": returnID, "user_id": userID, "created_at": now,
	}})

	encoded := make(map[string]int64, len(stock))
	for wid, units := range stock {
		encoded[strconv.FormatInt(wid, 10)] = units
	}
	updated := make(map[string]any, len(doc)+1)
	for k, v := range doc {
		if k != "_version" && k != "id" {
			updated[k] = v
		}
	}
	updated[movementStockField] = encoded
	err = s.sub.Commit(ctx, CommitRequest{
		Update: CommitUpdate{Entity: consumableEntity, ID: line.id, Version: &version, Data: updated},
		Append: appends,
	})
	if err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return rejection("stale_state", "the consumable stock changed concurrently: read it again and retry", x.state)
		}
		return classify(err, x.id)
	}
	return nil
}

func floatPtr(v float64) *float64 { return &v }
