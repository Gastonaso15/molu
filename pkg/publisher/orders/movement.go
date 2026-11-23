package orders

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// MovementType is the kind of stock movement.
type MovementType string

const (
	MovementEntry    MovementType = "entry"
	MovementExit     MovementType = "exit"
	MovementTransfer MovementType = "transfer"
	MovementAdjust   MovementType = "adjust"

	movementEntity        = "movement"
	movementRequestEntity = "movement_request"
	movementStockField    = "stock_by_warehouse"
	movementMaxUnits      = int64(999999999999)
)

// MovementInput is the payload of work_orders.RegisterMovement.
//
// Quantity is given in display units with at most two decimal places (stored
// internally as integer hundredths). For entry, exit and transfer it must be
// strictly positive; for adjust it is the new physical stock and may be zero.
// RequestID must be 32 lowercase hex characters when provided; if omitted, a
// fresh one is generated and returned so the caller can safely retry.
type MovementInput struct {
	Type              MovementType `json:"type"`
	ConsumableID      ID           `json:"consumable_id"`
	UserID            ID           `json:"user_id"`
	OriginWarehouseID ID           `json:"origin_warehouse_id,omitempty"`
	TargetWarehouseID ID           `json:"target_warehouse_id,omitempty"`
	Quantity          *float64     `json:"quantity"`
	Reason            string       `json:"reason"`
	Reference         string       `json:"reference,omitempty"`
	RequestID         string       `json:"request_id,omitempty"`
}

// MovementResult reports the committed (or replayed) stock movement.
type MovementResult struct {
	RequestID         string       `json:"request_id"`
	Type              MovementType `json:"type"`
	ConsumableID      string       `json:"consumable_id"`
	OriginWarehouseID string       `json:"origin_warehouse_id,omitempty"`
	TargetWarehouseID string       `json:"target_warehouse_id,omitempty"`
	Quantity          float64      `json:"quantity"`
	Delta             float64      `json:"delta"`
	StockBefore       float64      `json:"stock_before"`
	StockAfter        float64      `json:"stock_after"`
	TargetStockBefore *float64     `json:"target_stock_before,omitempty"`
	TargetStockAfter  *float64     `json:"target_stock_after,omitempty"`
	AlreadyProcessed  bool         `json:"already_processed,omitempty"`
}

// movementFingerprintPayload matches the canonical JSON shape hashed for the
// idempotency receipt (movement_request.fingerprint).
type movementFingerprintPayload struct {
	ResponsibleUserID int64
	Type              MovementType
	OriginWarehouseID int64
	TargetWarehouseID int64
	ConsumableID      int64
	Quantity          int64
	Reason            string
	Reference         string
	RequestID         string
}

// RegisterMovement applies one stock movement (entry, exit, transfer or adjust)
// atomically via xolu Commit: it updates consumable.stock_by_warehouse (in
// integer hundredths, guarded by _version) and appends both the movement audit
// record and an idempotency receipt (movement_request).
//
// Replaying the exact same request_id and payload returns AlreadyProcessed=true
// without writing again; reusing a request_id with different content is
// rejected as XOLU-INVALID_TRANSITION (details.rule="request_reused").
func (s *Service) RegisterMovement(ctx context.Context, in MovementInput) (MovementResult, error) {
	req, err := parseMovementInput(in)
	if err != nil {
		return MovementResult{}, err
	}
	if req.RequestID == "" {
		req.RequestID, err = newMovementRequestID()
		if err != nil {
			return MovementResult{}, fmt.Errorf("generate request_id: %w", err)
		}
	}
	receiptID, ok := movementReceiptID(req.RequestID)
	if !ok {
		return MovementResult{}, validationFrom([]fieldProblem{{"request_id", "must be 32 lowercase hexadecimal characters"}})
	}
	rawReq, err := json.Marshal(req)
	if err != nil {
		return MovementResult{}, fmt.Errorf("encode movement fingerprint: %w", err)
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(rawReq))

	s.movementMu.Lock()
	defer s.movementMu.Unlock()

	saved, err := s.checkMovementReceipt(ctx, receiptID, req.RequestID, fingerprint)
	if err != nil {
		return MovementResult{}, err
	}
	if saved {
		return s.replayedMovementResult(ctx, req), nil
	}

	consIDStr := strconv.FormatInt(req.ConsumableID, 10)
	consDoc, err := s.sub.GetEntity(ctx, consumableEntity, consIDStr)
	if err != nil {
		if isMissing(err) {
			return MovementResult{}, validationFrom([]fieldProblem{{"consumable_id", fmt.Sprintf("consumable %d does not exist", req.ConsumableID)}})
		}
		return MovementResult{}, classify(err, "")
	}
	if enabled, _ := consDoc["enabled"].(bool); !enabled {
		return MovementResult{}, rejection("consumable_disabled", fmt.Sprintf("consumable %d is disabled", req.ConsumableID), "")
	}
	version := int(intOf(consDoc["_version"]))
	if version <= 0 || int64(version) != intOf(consDoc["_version"]) {
		return MovementResult{}, fmt.Errorf("consumable %d has no valid _version: cannot update stock safely", req.ConsumableID)
	}

	stock, err := s.loadConsumableStockMap(ctx, req.ConsumableID, consDoc)
	if err != nil {
		return MovementResult{}, err
	}

	primaryID := req.TargetWarehouseID
	primaryField := "target_warehouse_id"
	if req.Type == MovementExit || req.Type == MovementTransfer {
		primaryID = req.OriginWarehouseID
		primaryField = "origin_warehouse_id"
	}
	primaryStock, err := s.checkMovementWarehouse(ctx, primaryID, stock, primaryField)
	if err != nil {
		return MovementResult{}, err
	}

	warehouseIDs := []int64{primaryID}
	before := []int64{primaryStock}
	after := []int64{primaryStock}

	switch req.Type {
	case MovementEntry:
		after[0] += req.Quantity
	case MovementExit:
		if primaryStock < req.Quantity {
			return MovementResult{}, rejection("insufficient_stock", "not enough stock in the origin warehouse", "")
		}
		after[0] -= req.Quantity
	case MovementTransfer:
		if primaryStock < req.Quantity {
			return MovementResult{}, rejection("insufficient_stock", "not enough stock in the origin warehouse", "")
		}
		targetStock, err := s.checkMovementWarehouse(ctx, req.TargetWarehouseID, stock, "target_warehouse_id")
		if err != nil {
			return MovementResult{}, err
		}
		warehouseIDs = append(warehouseIDs, req.TargetWarehouseID)
		before = append(before, targetStock)
		after[0] -= req.Quantity
		after = append(after, targetStock+req.Quantity)
	case MovementAdjust:
		after[0] = req.Quantity
	}

	for _, units := range after {
		if units < 0 || units > movementMaxUnits {
			return MovementResult{}, rejection("stock_overflow", "resulting stock is outside the allowed range", "")
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	movementData := map[string]any{
		"request_id":    req.RequestID,
		"consumable_id": req.ConsumableID,
		"type":          string(req.Type),
		"quantity":      float64(req.Quantity) / 100,
		"delta":         float64(after[0]-before[0]) / 100,
		"stock_before":  float64(before[0]) / 100,
		"stock_after":   float64(after[0]) / 100,
		"user_id":       req.ResponsibleUserID,
		"reason":        req.Reason,
		"reference":     req.Reference,
		"created_at":    now,
	}
	if req.OriginWarehouseID > 0 {
		movementData["origin_warehouse_id"] = req.OriginWarehouseID
	}
	if req.TargetWarehouseID > 0 {
		movementData["target_warehouse_id"] = req.TargetWarehouseID
	}
	if len(warehouseIDs) == 2 {
		movementData["target_stock_before"] = float64(before[1]) / 100
		movementData["target_stock_after"] = float64(after[1]) / 100
	}

	for i, wid := range warehouseIDs {
		stock[wid] = after[i]
	}
	encodedStock := make(map[string]int64, len(stock))
	for wid, units := range stock {
		encodedStock[strconv.FormatInt(wid, 10)] = units
	}

	updatedConsumable := make(map[string]any, len(consDoc)+1)
	for k, v := range consDoc {
		if k != "_version" && k != "id" {
			updatedConsumable[k] = v
		}
	}
	updatedConsumable[movementStockField] = encodedStock

	saveErr := s.sub.Commit(ctx, CommitRequest{
		Update: CommitUpdate{
			Entity:  consumableEntity,
			ID:      req.ConsumableID,
			Version: &version,
			Data:    updatedConsumable,
		},
		Append: []CommitAppend{
			{Entity: movementEntity, Data: movementData},
			{
				Entity: movementRequestEntity,
				ID:     &receiptID,
				Data: map[string]any{
					"request_id":  req.RequestID,
					"fingerprint": fingerprint,
					"user_id":     req.ResponsibleUserID,
					"created_at":  now,
				},
			},
		},
	})
	if saveErr == nil {
		return buildMovementResult(req, before, after, false), nil
	}

	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	confirmed, receiptErr := s.checkMovementReceipt(checkCtx, receiptID, req.RequestID, fingerprint)
	if confirmed {
		return buildMovementResult(req, before, after, false), nil
	}
	if receiptErr != nil {
		var te *TransitionError
		if errors.As(receiptErr, &te) {
			return MovementResult{}, receiptErr
		}
		return MovementResult{}, &UnavailableError{Cause: fmt.Errorf("movement commit outcome unknown (retry with request_id %s): %w", req.RequestID, receiptErr)}
	}

	var xe *XoluError
	if errors.As(saveErr, &xe) {
		switch xe.Status {
		case 409:
			return MovementResult{}, rejection("stale_state", "the consumable stock changed concurrently: read it again and retry", "")
		case 400, 401, 403, 404, 422:
			return MovementResult{}, fmt.Errorf("commit movement: %w", saveErr)
		}
	}
	return MovementResult{}, &UnavailableError{Cause: fmt.Errorf("could not confirm movement (retry safely with request_id %s): %w", req.RequestID, saveErr)}
}

func parseMovementInput(in MovementInput) (movementFingerprintPayload, error) {
	var problems []fieldProblem
	mt := MovementType(strings.TrimSpace(string(in.Type)))
	validType := true
	switch mt {
	case MovementEntry, MovementExit, MovementTransfer, MovementAdjust:
	default:
		validType = false
		problems = append(problems, fieldProblem{"type", "must be entry, exit, transfer or adjust"})
	}

	consID, ok := parsePositiveID(in.ConsumableID)
	if !ok {
		problems = append(problems, fieldProblem{"consumable_id", "must be a positive id"})
	}
	userID, ok := parsePositiveID(in.UserID)
	if !ok {
		problems = append(problems, fieldProblem{"user_id", "must be a positive id"})
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		problems = append(problems, fieldProblem{"reason", "required"})
	}
	reference := strings.TrimSpace(in.Reference)

	var units int64
	if in.Quantity == nil {
		problems = append(problems, fieldProblem{"quantity", "required"})
	} else {
		u, err := movementStoredUnits(*in.Quantity)
		if err != nil {
			problems = append(problems, fieldProblem{"quantity", "must be a non-negative number with at most two decimal places"})
		} else if validType && mt != MovementAdjust && u == 0 {
			problems = append(problems, fieldProblem{"quantity", "must be greater than zero"})
		} else {
			units = u
		}
	}

	var originID, targetID int64
	if strings.TrimSpace(string(in.OriginWarehouseID)) != "" {
		id, ok := parsePositiveID(in.OriginWarehouseID)
		if !ok {
			problems = append(problems, fieldProblem{"origin_warehouse_id", "must be a positive id"})
		} else {
			originID = id
		}
	}
	if strings.TrimSpace(string(in.TargetWarehouseID)) != "" {
		id, ok := parsePositiveID(in.TargetWarehouseID)
		if !ok {
			problems = append(problems, fieldProblem{"target_warehouse_id", "must be a positive id"})
		} else {
			targetID = id
		}
	}

	if validType {
		switch mt {
		case MovementEntry, MovementAdjust:
			if targetID == 0 {
				problems = append(problems, fieldProblem{"target_warehouse_id", "required for " + string(mt)})
			}
			if originID != 0 {
				problems = append(problems, fieldProblem{"origin_warehouse_id", "must be omitted for " + string(mt)})
			}
		case MovementExit:
			if originID == 0 {
				problems = append(problems, fieldProblem{"origin_warehouse_id", "required for exit"})
			}
			if targetID != 0 {
				problems = append(problems, fieldProblem{"target_warehouse_id", "must be omitted for exit"})
			}
		case MovementTransfer:
			if originID == 0 {
				problems = append(problems, fieldProblem{"origin_warehouse_id", "required for transfer"})
			}
			if targetID == 0 {
				problems = append(problems, fieldProblem{"target_warehouse_id", "required for transfer"})
			} else if originID != 0 && originID == targetID {
				problems = append(problems, fieldProblem{"target_warehouse_id", "must differ from origin_warehouse_id"})
			}
		}
	}

	reqID := strings.TrimSpace(in.RequestID)
	if reqID != "" {
		if _, ok := movementReceiptID(reqID); !ok {
			problems = append(problems, fieldProblem{"request_id", "must be 32 lowercase hexadecimal characters"})
		}
	}

	if err := validationFrom(problems); err != nil {
		return movementFingerprintPayload{}, err
	}
	return movementFingerprintPayload{
		ResponsibleUserID: userID,
		Type:              mt,
		OriginWarehouseID: originID,
		TargetWarehouseID: targetID,
		ConsumableID:      consID,
		Quantity:          units,
		Reason:            reason,
		Reference:         reference,
		RequestID:         reqID,
	}, nil
}

func parsePositiveID(id ID) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(string(id)), 10, 64)
	return n, err == nil && n > 0
}

func newMovementRequestID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", data), nil
}

func movementReceiptID(requestID string) (int64, bool) {
	if len(requestID) != 32 {
		return 0, false
	}
	for _, c := range requestID {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(requestID[:13], 16, 64)
	return id + 1, err == nil
}

func movementStoredUnits(stock float64) (int64, error) {
	if math.IsNaN(stock) || math.IsInf(stock, 0) || stock < 0 {
		return 0, errors.New("invalid stock value")
	}
	scaled := stock * 100
	rounded := math.Round(scaled)
	if rounded > float64(movementMaxUnits) || math.Abs(scaled-rounded) > 0.001 {
		return 0, errors.New("invalid stock precision or overflow")
	}
	return int64(rounded), nil
}

func (s *Service) checkMovementReceipt(ctx context.Context, receiptID int64, requestID, fingerprint string) (bool, error) {
	doc, err := s.sub.GetEntity(ctx, movementRequestEntity, strconv.FormatInt(receiptID, 10))
	if err != nil {
		if isMissing(err) {
			return false, nil
		}
		return false, classify(err, "")
	}
	storedReqID, _ := doc["request_id"].(string)
	storedFP, _ := doc["fingerprint"].(string)
	if storedReqID != requestID || storedFP != fingerprint {
		return false, rejection("request_reused", "request_id was already used for a different movement", "")
	}
	return true, nil
}

func (s *Service) replayedMovementResult(ctx context.Context, req movementFingerprintPayload) MovementResult {
	res := MovementResult{
		RequestID:        req.RequestID,
		Type:             req.Type,
		ConsumableID:     strconv.FormatInt(req.ConsumableID, 10),
		Quantity:         float64(req.Quantity) / 100,
		AlreadyProcessed: true,
	}
	if req.OriginWarehouseID > 0 {
		res.OriginWarehouseID = strconv.FormatInt(req.OriginWarehouseID, 10)
	}
	if req.TargetWarehouseID > 0 {
		res.TargetWarehouseID = strconv.FormatInt(req.TargetWarehouseID, 10)
	}
	rows, err := s.sub.FindEntities(ctx, movementEntity, EntityQuery{
		Where: []Condition{{Field: "request_id", Op: "=", Value: req.RequestID}},
		Limit: 1,
	})
	if err == nil && len(rows) > 0 {
		m := rows[0]
		res.Delta = floatOf(m["delta"])
		res.StockBefore = floatOf(m["stock_before"])
		res.StockAfter = floatOf(m["stock_after"])
		if _, ok := m["target_stock_before"]; ok {
			tb := floatOf(m["target_stock_before"])
			res.TargetStockBefore = &tb
		}
		if _, ok := m["target_stock_after"]; ok {
			ta := floatOf(m["target_stock_after"])
			res.TargetStockAfter = &ta
		}
	}
	return res
}

func (s *Service) loadConsumableStockMap(ctx context.Context, consumableID int64, consDoc map[string]any) (map[int64]int64, error) {
	if raw, migrated := consDoc[movementStockField]; migrated && raw != nil {
		var byWarehouse map[string]any
		if err := roundTrip(raw, &byWarehouse); err != nil {
			return nil, fmt.Errorf("decode %s: %w", movementStockField, err)
		}
		stock := make(map[int64]int64, len(byWarehouse))
		for k, v := range byWarehouse {
			wid, err := strconv.ParseInt(k, 10, 64)
			if err != nil || wid <= 0 {
				return nil, fmt.Errorf("invalid warehouse key %q in %s", k, movementStockField)
			}
			units := intOf(v)
			if units < 0 || units > movementMaxUnits {
				return nil, fmt.Errorf("invalid stock value for warehouse %d", wid)
			}
			stock[wid] = units
		}
		return stock, nil
	}

	// Unmigrated consumable: collect legacy balances stored on warehouse documents.
	stock := map[int64]int64{}
	rows, _, err := s.scanCatalog(ctx, warehouseEntity)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		wid := intOf(row["id"])
		if wid <= 0 {
			continue
		}
		var balances []struct {
			ConsumableID int64   `json:"consumable_id"`
			Stock        float64 `json:"stock"`
		}
		if err := roundTrip(row["consumables"], &balances); err != nil {
			continue
		}
		for _, b := range balances {
			if b.ConsumableID == consumableID {
				if units, err := movementStoredUnits(b.Stock); err == nil {
					stock[wid] = units
				}
			}
		}
	}
	return stock, nil
}

func (s *Service) checkMovementWarehouse(ctx context.Context, warehouseID int64, stock map[int64]int64, field string) (int64, error) {
	doc, err := s.sub.GetEntity(ctx, warehouseEntity, strconv.FormatInt(warehouseID, 10))
	if err != nil {
		if isMissing(err) {
			return 0, validationFrom([]fieldProblem{{field, fmt.Sprintf("warehouse %d does not exist", warehouseID)}})
		}
		return 0, classify(err, "")
	}
	if enabled, _ := doc["enabled"].(bool); !enabled {
		return 0, rejection("warehouse_disabled", fmt.Sprintf("warehouse %d is disabled", warehouseID), "")
	}
	return stock[warehouseID], nil
}

func buildMovementResult(req movementFingerprintPayload, before, after []int64, already bool) MovementResult {
	res := MovementResult{
		RequestID:        req.RequestID,
		Type:             req.Type,
		ConsumableID:     strconv.FormatInt(req.ConsumableID, 10),
		Quantity:         float64(req.Quantity) / 100,
		Delta:            float64(after[0]-before[0]) / 100,
		StockBefore:      float64(before[0]) / 100,
		StockAfter:       float64(after[0]) / 100,
		AlreadyProcessed: already,
	}
	if req.OriginWarehouseID > 0 {
		res.OriginWarehouseID = strconv.FormatInt(req.OriginWarehouseID, 10)
	}
	if req.TargetWarehouseID > 0 {
		res.TargetWarehouseID = strconv.FormatInt(req.TargetWarehouseID, 10)
	}
	if len(before) == 2 && len(after) == 2 {
		tb := float64(before[1]) / 100
		ta := float64(after[1]) / 100
		res.TargetStockBefore = &tb
		res.TargetStockAfter = &ta
	}
	return res
}
