package orders

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ConsumeFromOrderInput is the payload of work_orders.ConsumeFromOrder.
type ConsumeFromOrderInput struct {
	OrderID      string   `json:"order_id"`
	ConsumableID ID       `json:"consumable_id"`
	UserID       ID       `json:"user_id"`
	Quantity     *float64 `json:"quantity"`
	Reason       string   `json:"reason,omitempty"`
	Reference    string   `json:"reference,omitempty"`
	RequestID    string   `json:"request_id,omitempty"`
}

// ReturnLeftoversInput is the payload of work_orders.ReturnLeftovers. When
// Quantity is omitted, all remaining stock of the consumable in the order's
// mobile warehouse is transferred back to TargetWarehouseID.
type ReturnLeftoversInput struct {
	OrderID           string   `json:"order_id"`
	ConsumableID      ID       `json:"consumable_id"`
	TargetWarehouseID ID       `json:"target_warehouse_id"`
	UserID            ID       `json:"user_id"`
	Quantity          *float64 `json:"quantity,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	Reference         string   `json:"reference,omitempty"`
	RequestID         string   `json:"request_id,omitempty"`
}

// OrderMovementResult wraps a stock movement tied to a work order's mobile
// warehouse.
type OrderMovementResult struct {
	OrderID           string         `json:"order_id"`
	MobileWarehouseID string         `json:"mobile_warehouse_id"`
	Movement          MovementResult `json:"movement"`
}

// ConsumeFromOrder records consumption of a consumable from an open work
// order's mobile warehouse (an atomic exit movement via RegisterMovement).
// Completed orders and orders without a mobile warehouse are rejected. When the
// order follows a template whose extra consumables are disabled, only
// consumables listed on the template may be consumed.
func (s *Service) ConsumeFromOrder(ctx context.Context, in ConsumeFromOrderInput) (OrderMovementResult, error) {
	orderID := strings.TrimSpace(in.OrderID)
	var problems []fieldProblem
	if orderID == "" {
		problems = append(problems, fieldProblem{"order_id", "required"})
	}
	consID, ok := parsePositiveID(in.ConsumableID)
	if !ok {
		problems = append(problems, fieldProblem{"consumable_id", "must be a positive id"})
	}
	if _, ok := parsePositiveID(in.UserID); !ok {
		problems = append(problems, fieldProblem{"user_id", "must be a positive id"})
	}
	if in.Quantity == nil {
		problems = append(problems, fieldProblem{"quantity", "required"})
	} else if u, err := movementStoredUnits(*in.Quantity); err != nil || u <= 0 {
		problems = append(problems, fieldProblem{"quantity", "must be a positive number with at most two decimal places"})
	}
	reqID := strings.TrimSpace(in.RequestID)
	if reqID != "" {
		if _, ok := movementReceiptID(reqID); !ok {
			problems = append(problems, fieldProblem{"request_id", "must be 32 lowercase hexadecimal characters"})
		}
	}
	if err := validationFrom(problems); err != nil {
		return OrderMovementResult{}, err
	}

	doc, err := s.sub.GetEntity(ctx, EntityType, orderID)
	if err != nil {
		return OrderMovementResult{}, classify(err, orderID)
	}
	order := orderFromDoc(orderID, doc)
	if order.Terminal {
		return OrderMovementResult{}, rejection("final_state", fmt.Sprintf("cannot consume stock from completed order %q", orderID), order.State)
	}
	if order.MobileWarehouseID == "" {
		return OrderMovementResult{}, rejection("no_mobile_warehouse", "the work order has no mobile warehouse", order.State)
	}

	consIDStr := strconv.FormatInt(consID, 10)
	if templateOrder(doc) {
		if err := s.checkTemplateConsumableAllowed(ctx, idString(doc["template_id"]), consIDStr, order.State); err != nil {
			return OrderMovementResult{}, err
		}
	}

	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "Consumo en orden #" + orderID
	}
	reference := strings.TrimSpace(in.Reference)
	if reference == "" {
		reference = "WO-" + orderID
	}

	mv, err := s.RegisterMovement(ctx, MovementInput{
		Type:              MovementExit,
		ConsumableID:      ID(consIDStr),
		UserID:            in.UserID,
		OriginWarehouseID: ID(order.MobileWarehouseID),
		Quantity:          in.Quantity,
		Reason:            reason,
		Reference:         reference,
		RequestID:         reqID,
	})
	if err != nil {
		return OrderMovementResult{}, err
	}
	return OrderMovementResult{
		OrderID:           orderID,
		MobileWarehouseID: order.MobileWarehouseID,
		Movement:          mv,
	}, nil
}

// ReturnLeftovers transfers leftover stock from a work order's mobile warehouse
// back to a destination warehouse (an atomic transfer via RegisterMovement).
// It works on both open and completed orders. When Quantity is omitted, all
// remaining stock of that consumable in the mobile warehouse is returned.
func (s *Service) ReturnLeftovers(ctx context.Context, in ReturnLeftoversInput) (OrderMovementResult, error) {
	orderID := strings.TrimSpace(in.OrderID)
	var problems []fieldProblem
	if orderID == "" {
		problems = append(problems, fieldProblem{"order_id", "required"})
	}
	consID, ok := parsePositiveID(in.ConsumableID)
	if !ok {
		problems = append(problems, fieldProblem{"consumable_id", "must be a positive id"})
	}
	targetID, ok := parsePositiveID(in.TargetWarehouseID)
	if !ok {
		problems = append(problems, fieldProblem{"target_warehouse_id", "must be a positive id"})
	}
	if _, ok := parsePositiveID(in.UserID); !ok {
		problems = append(problems, fieldProblem{"user_id", "must be a positive id"})
	}
	if in.Quantity != nil {
		if u, err := movementStoredUnits(*in.Quantity); err != nil || u <= 0 {
			problems = append(problems, fieldProblem{"quantity", "must be a positive number with at most two decimal places"})
		}
	}
	reqID := strings.TrimSpace(in.RequestID)
	if reqID != "" {
		if _, ok := movementReceiptID(reqID); !ok {
			problems = append(problems, fieldProblem{"request_id", "must be 32 lowercase hexadecimal characters"})
		}
	}
	if err := validationFrom(problems); err != nil {
		return OrderMovementResult{}, err
	}

	doc, err := s.sub.GetEntity(ctx, EntityType, orderID)
	if err != nil {
		return OrderMovementResult{}, classify(err, orderID)
	}
	order := orderFromDoc(orderID, doc)
	if order.MobileWarehouseID == "" {
		return OrderMovementResult{}, rejection("no_mobile_warehouse", "the work order has no mobile warehouse", order.State)
	}
	mobileID, err := strconv.ParseInt(order.MobileWarehouseID, 10, 64)
	if err != nil || mobileID <= 0 {
		return OrderMovementResult{}, fmt.Errorf("work order %s has an invalid mobile_warehouse_id %q", orderID, order.MobileWarehouseID)
	}
	if targetID == mobileID {
		return OrderMovementResult{}, validationFrom([]fieldProblem{{"target_warehouse_id", "must differ from the order's mobile warehouse"}})
	}

	consIDStr := strconv.FormatInt(consID, 10)
	qty := in.Quantity
	if qty == nil {
		resolved, err := s.resolveLeftoverQuantity(ctx, consID, mobileID, reqID, order.State)
		if err != nil {
			return OrderMovementResult{}, err
		}
		qty = &resolved
	}

	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "Devolucion de sobrantes orden #" + orderID
	}
	reference := strings.TrimSpace(in.Reference)
	if reference == "" {
		reference = "WO-" + orderID
	}

	mv, err := s.RegisterMovement(ctx, MovementInput{
		Type:              MovementTransfer,
		ConsumableID:      ID(consIDStr),
		UserID:            in.UserID,
		OriginWarehouseID: ID(order.MobileWarehouseID),
		TargetWarehouseID: ID(strconv.FormatInt(targetID, 10)),
		Quantity:          qty,
		Reason:            reason,
		Reference:         reference,
		RequestID:         reqID,
	})
	if err != nil {
		return OrderMovementResult{}, err
	}
	return OrderMovementResult{
		OrderID:           orderID,
		MobileWarehouseID: order.MobileWarehouseID,
		Movement:          mv,
	}, nil
}

func (s *Service) checkTemplateConsumableAllowed(ctx context.Context, templateID, consumableID, state string) error {
	if templateID == "" || templateID == "0" {
		return nil
	}
	tpl, err := s.loadTemplate(ctx, templateID)
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return nil // unreadable template does not block consumption
		}
		return err
	}
	if tpl.ExtraConsumablesAllowed || len(tpl.Consumables) == 0 {
		return nil
	}
	for _, c := range tpl.Consumables {
		if c.ConsumableID == consumableID {
			return nil
		}
	}
	return rejection("consumable_not_allowed",
		fmt.Sprintf("consumable %s is not listed on the template and extra consumables are disabled", consumableID),
		state)
}

func (s *Service) resolveLeftoverQuantity(ctx context.Context, consumableID, mobileWarehouseID int64, reqID, state string) (float64, error) {
	// If an explicit request_id was already committed, recover its stored
	// quantity so RegisterMovement can verify the fingerprint and return
	// already_processed=true even after the mobile warehouse balance hit zero.
	if reqID != "" {
		if receiptID, ok := movementReceiptID(reqID); ok {
			receiptDoc, err := s.sub.GetEntity(ctx, movementRequestEntity, strconv.FormatInt(receiptID, 10))
			if err == nil {
				storedReqID, _ := receiptDoc["request_id"].(string)
				if storedReqID != reqID {
					return 0, rejection("request_reused", "request_id was already used for a different movement", state)
				}
				rows, err := s.sub.FindEntities(ctx, movementEntity, EntityQuery{
					Where: []Condition{{Field: "request_id", Op: "=", Value: reqID}},
					Limit: 1,
				})
				if err == nil && len(rows) > 0 {
					if q := floatOf(rows[0]["quantity"]); q > 0 {
						return q, nil
					}
				}
			} else if !isMissing(err) {
				return 0, classify(err, "")
			}
		}
	}

	consDoc, err := s.sub.GetEntity(ctx, consumableEntity, strconv.FormatInt(consumableID, 10))
	if err != nil {
		if isMissing(err) {
			return 0, validationFrom([]fieldProblem{{"consumable_id", fmt.Sprintf("consumable %d does not exist", consumableID)}})
		}
		return 0, classify(err, "")
	}
	if enabled, _ := consDoc["enabled"].(bool); !enabled {
		return 0, rejection("consumable_disabled", fmt.Sprintf("consumable %d is disabled", consumableID), state)
	}
	stock, err := s.loadConsumableStockMap(ctx, consumableID, consDoc)
	if err != nil {
		return 0, err
	}
	units := stock[mobileWarehouseID]
	if units <= 0 {
		return 0, rejection("no_leftovers", "the mobile warehouse has no stock of this consumable to return", state)
	}
	return float64(units) / 100, nil
}
