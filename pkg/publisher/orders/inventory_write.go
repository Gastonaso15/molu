package orders

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxCatalogNameRunes = 200

// CreateWarehouseInput is the payload of work_orders.CreateWarehouse.
// Location must be GPS coordinates ("lat,lng"); Enabled defaults to true.
type CreateWarehouseInput struct {
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	Location          string `json:"location"`
	ResponsibleUserID ID     `json:"responsible_user_id"`
	Enabled           *bool  `json:"enabled,omitempty"`
}

// UpdateWarehouseInput is the payload of work_orders.UpdateWarehouse. Only
// provided fields change. Disabling a warehouse that still holds stock is
// rejected with XOLU-INVALID_TRANSITION (details.rule="warehouse_has_stock").
type UpdateWarehouseInput struct {
	WarehouseID       ID      `json:"warehouse_id"`
	Name              *string `json:"name,omitempty"`
	Description       *string `json:"description,omitempty"`
	Location          *string `json:"location,omitempty"`
	ResponsibleUserID *ID     `json:"responsible_user_id,omitempty"`
	Enabled           *bool   `json:"enabled,omitempty"`
}

// CreateConsumableInput is the payload of work_orders.CreateConsumable.
// Enabled defaults to true and StockMin defaults to 0. Initial stock is added
// through work_orders.RegisterMovement.
type CreateConsumableInput struct {
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	ConsumableTypeID ID       `json:"consumable_type_id,omitempty"`
	UnitID           ID       `json:"unit_id"`
	StockMin         *float64 `json:"stock_min,omitempty"`
	Enabled          *bool    `json:"enabled,omitempty"`
}

// UpdateConsumableInput is the payload of work_orders.UpdateConsumable. Only
// provided fields change; stock quantities are never edited directly here.
// Setting ConsumableTypeID to "" or "0" clears the consumable type.
type UpdateConsumableInput struct {
	ConsumableID     ID       `json:"consumable_id"`
	Name             *string  `json:"name,omitempty"`
	Description      *string  `json:"description,omitempty"`
	ConsumableTypeID *ID      `json:"consumable_type_id,omitempty"`
	UnitID           *ID      `json:"unit_id,omitempty"`
	StockMin         *float64 `json:"stock_min,omitempty"`
	Enabled          *bool    `json:"enabled,omitempty"`
}

// CreateWarehouse creates a fixed warehouse in the warehouse entity, checking
// name, GPS location and active responsible user.
func (s *Service) CreateWarehouse(ctx context.Context, in CreateWarehouseInput) (Warehouse, error) {
	name := strings.TrimSpace(in.Name)
	var problems []fieldProblem
	if name == "" {
		problems = append(problems, fieldProblem{"name", "required"})
	} else if utf8.RuneCountInString(name) > maxCatalogNameRunes {
		problems = append(problems, fieldProblem{"name", fmt.Sprintf("must be at most %d characters", maxCatalogNameRunes)})
	}

	loc, locOK := normalizeGPS(in.Location)
	if strings.TrimSpace(in.Location) == "" {
		problems = append(problems, fieldProblem{"location", "required: latitude,longitude"})
	} else if !locOK {
		problems = append(problems, fieldProblem{"location", `must be valid "latitude,longitude" coordinates`})
	}

	respID, ok := parsePositiveID(in.ResponsibleUserID)
	if !ok {
		problems = append(problems, fieldProblem{"responsible_user_id", "must be a positive id"})
	}
	if err := validationFrom(problems); err != nil {
		return Warehouse{}, err
	}

	names, userProblems, err := s.validateActiveUsers(ctx, map[string][]int64{"responsible_user_id": {respID}})
	if err != nil {
		return Warehouse{}, err
	}
	if err := validationFrom(userProblems); err != nil {
		return Warehouse{}, err
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	doc := map[string]any{
		"name":                name,
		"description":         strings.TrimSpace(in.Description),
		"location":            loc,
		"responsible_user_id": respID,
		"enabled":             enabled,
		"consumables":         []any{},
	}
	id, err := s.sub.CreateEntity(ctx, warehouseEntity, doc)
	if err != nil {
		return Warehouse{}, classify(err, "")
	}
	w := warehouseFromDoc(id, doc)
	if w.Responsible != nil {
		w.Responsible.Name = names[w.Responsible.UserID]
	}
	return w, nil
}

// UpdateWarehouse updates an existing warehouse (partial PATCH guarded by
// _version when present). When disabling (enabled=false), the warehouse must
// hold zero stock; the check and update are serialized with movementMu so a
// concurrent movement cannot race with disabling.
func (s *Service) UpdateWarehouse(ctx context.Context, in UpdateWarehouseInput) (Warehouse, error) {
	if !positiveID(in.WarehouseID) {
		return Warehouse{}, &ValidationError{Fields: []string{"warehouse_id"}, Reason: "must be a positive id"}
	}
	if in.Name == nil && in.Description == nil && in.Location == nil && in.ResponsibleUserID == nil && in.Enabled == nil {
		return Warehouse{}, validationFrom([]fieldProblem{{"warehouse_id", "provide at least one field to update"}})
	}

	var problems []fieldProblem
	var newName, newLoc string
	if in.Name != nil {
		newName = strings.TrimSpace(*in.Name)
		if newName == "" {
			problems = append(problems, fieldProblem{"name", "must not be empty"})
		} else if utf8.RuneCountInString(newName) > maxCatalogNameRunes {
			problems = append(problems, fieldProblem{"name", fmt.Sprintf("must be at most %d characters", maxCatalogNameRunes)})
		}
	}
	if in.Location != nil {
		loc, ok := normalizeGPS(*in.Location)
		if !ok {
			problems = append(problems, fieldProblem{"location", `must be valid "latitude,longitude" coordinates`})
		} else {
			newLoc = loc
		}
	}
	var newRespID int64
	if in.ResponsibleUserID != nil {
		id, ok := parsePositiveID(*in.ResponsibleUserID)
		if !ok {
			problems = append(problems, fieldProblem{"responsible_user_id", "must be a positive id"})
		} else {
			newRespID = id
		}
	}
	if err := validationFrom(problems); err != nil {
		return Warehouse{}, err
	}

	id := strings.TrimSpace(string(in.WarehouseID))
	disabling := in.Enabled != nil && !*in.Enabled
	if disabling {
		s.movementMu.Lock()
		defer s.movementMu.Unlock()
	}

	doc, err := s.sub.GetEntity(ctx, warehouseEntity, id)
	if err != nil {
		if isMissing(err) {
			return Warehouse{}, &NotFoundError{ID: id, Kind: "warehouse"}
		}
		return Warehouse{}, classify(err, "")
	}

	var respName string
	if newRespID > 0 {
		names, userProblems, err := s.validateActiveUsers(ctx, map[string][]int64{"responsible_user_id": {newRespID}})
		if err != nil {
			return Warehouse{}, err
		}
		if err := validationFrom(userProblems); err != nil {
			return Warehouse{}, err
		}
		respName = names[strconv.FormatInt(newRespID, 10)]
	}

	if disabling {
		lines, err := s.warehouseStock(ctx, id, doc)
		if err != nil {
			return Warehouse{}, err
		}
		for _, line := range lines {
			if line.Quantity > 0 {
				return Warehouse{}, rejection("warehouse_has_stock",
					"cannot disable a warehouse that still holds stock: transfer or exit its stock first", "")
			}
		}
	}

	patch := map[string]any{}
	if in.Name != nil {
		patch["name"] = newName
	}
	if in.Description != nil {
		patch["description"] = strings.TrimSpace(*in.Description)
	}
	if in.Location != nil {
		patch["location"] = newLoc
	}
	if newRespID > 0 {
		patch["responsible_user_id"] = newRespID
	}
	if in.Enabled != nil {
		patch["enabled"] = *in.Enabled
	}
	if v := intOf(doc["_version"]); v > 0 {
		patch["_version"] = v
	}

	if err := s.sub.PatchEntity(ctx, warehouseEntity, id, patch); err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return Warehouse{}, rejection("stale_state", "the warehouse changed while it was being updated: read it again and retry", "")
		}
		return Warehouse{}, classify(err, "")
	}
	for k, v := range patch {
		if k != "_version" {
			doc[k] = v
		}
	}
	w := warehouseFromDoc(id, doc)
	if w.Responsible != nil {
		if respName != "" {
			w.Responsible.Name = respName
		} else {
			w.Responsible.Name = s.userNames(ctx)(w.Responsible.UserID)
		}
	}
	return w, nil
}

// CreateConsumable creates a supply item in the consumable entity with a
// validated unit, optional type, non-negative stock_min and empty
// stock_by_warehouse map.
func (s *Service) CreateConsumable(ctx context.Context, in CreateConsumableInput) (Consumable, error) {
	name := strings.TrimSpace(in.Name)
	var problems []fieldProblem
	if name == "" {
		problems = append(problems, fieldProblem{"name", "required"})
	} else if utf8.RuneCountInString(name) > maxCatalogNameRunes {
		problems = append(problems, fieldProblem{"name", fmt.Sprintf("must be at most %d characters", maxCatalogNameRunes)})
	}
	unitID, ok := parsePositiveID(in.UnitID)
	if !ok {
		problems = append(problems, fieldProblem{"unit_id", "must be a positive id"})
	}
	var typeID int64
	if strings.TrimSpace(string(in.ConsumableTypeID)) != "" {
		id, ok := parsePositiveID(in.ConsumableTypeID)
		if !ok {
			problems = append(problems, fieldProblem{"consumable_type_id", "must be a positive id"})
		} else {
			typeID = id
		}
	}
	stockMin := 0.0
	if in.StockMin != nil {
		if math.IsNaN(*in.StockMin) || math.IsInf(*in.StockMin, 0) || *in.StockMin < 0 {
			problems = append(problems, fieldProblem{"stock_min", "must be a non-negative number"})
		} else {
			stockMin = *in.StockMin
		}
	}
	if err := validationFrom(problems); err != nil {
		return Consumable{}, err
	}

	unitName, typeName, refProblems, err := s.validateConsumableRefs(ctx, unitID, typeID)
	if err != nil {
		return Consumable{}, err
	}
	if err := validationFrom(refProblems); err != nil {
		return Consumable{}, err
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	doc := map[string]any{
		"name":               name,
		"description":        strings.TrimSpace(in.Description),
		"consumable_unit":    unitID,
		"stock_min":          stockMin,
		"enabled":            enabled,
		"stock_by_warehouse": map[string]int64{},
	}
	if typeID > 0 {
		doc["consumable_type_id"] = typeID
	}
	id, err := s.sub.CreateEntity(ctx, consumableEntity, doc)
	if err != nil {
		return Consumable{}, classify(err, "")
	}
	c := Consumable{
		ID:          id,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Unit:        &Named{ID: strconv.FormatInt(unitID, 10), Name: unitName},
		StockMin:    stockMin,
		Enabled:     enabled,
	}
	if typeID > 0 {
		c.Type = &Named{ID: strconv.FormatInt(typeID, 10), Name: typeName}
	}
	return c, nil
}

// UpdateConsumable updates a consumable's metadata (partial PATCH guarded by
// _version when present). Stock quantities are managed exclusively via
// RegisterMovement.
func (s *Service) UpdateConsumable(ctx context.Context, in UpdateConsumableInput) (Consumable, error) {
	if !positiveID(in.ConsumableID) {
		return Consumable{}, &ValidationError{Fields: []string{"consumable_id"}, Reason: "must be a positive id"}
	}
	if in.Name == nil && in.Description == nil && in.ConsumableTypeID == nil && in.UnitID == nil && in.StockMin == nil && in.Enabled == nil {
		return Consumable{}, validationFrom([]fieldProblem{{"consumable_id", "provide at least one field to update"}})
	}

	var problems []fieldProblem
	var newName string
	if in.Name != nil {
		newName = strings.TrimSpace(*in.Name)
		if newName == "" {
			problems = append(problems, fieldProblem{"name", "must not be empty"})
		} else if utf8.RuneCountInString(newName) > maxCatalogNameRunes {
			problems = append(problems, fieldProblem{"name", fmt.Sprintf("must be at most %d characters", maxCatalogNameRunes)})
		}
	}
	var newUnitID int64
	if in.UnitID != nil {
		id, ok := parsePositiveID(*in.UnitID)
		if !ok {
			problems = append(problems, fieldProblem{"unit_id", "must be a positive id"})
		} else {
			newUnitID = id
		}
	}
	var setType bool
	var newTypeID int64
	if in.ConsumableTypeID != nil {
		setType = true
		raw := strings.TrimSpace(string(*in.ConsumableTypeID))
		if raw != "" && raw != "0" {
			id, ok := parsePositiveID(*in.ConsumableTypeID)
			if !ok {
				problems = append(problems, fieldProblem{"consumable_type_id", "must be a positive id, or empty to clear"})
			} else {
				newTypeID = id
			}
		}
	}
	if in.StockMin != nil {
		if math.IsNaN(*in.StockMin) || math.IsInf(*in.StockMin, 0) || *in.StockMin < 0 {
			problems = append(problems, fieldProblem{"stock_min", "must be a non-negative number"})
		}
	}
	if err := validationFrom(problems); err != nil {
		return Consumable{}, err
	}

	id := strings.TrimSpace(string(in.ConsumableID))
	doc, err := s.sub.GetEntity(ctx, consumableEntity, id)
	if err != nil {
		if isMissing(err) {
			return Consumable{}, &NotFoundError{ID: id, Kind: "consumable"}
		}
		return Consumable{}, classify(err, "")
	}

	_, _, refProblems, err := s.validateConsumableRefs(ctx, newUnitID, newTypeID)
	if err != nil {
		return Consumable{}, err
	}
	if err := validationFrom(refProblems); err != nil {
		return Consumable{}, err
	}

	patch := map[string]any{}
	if in.Name != nil {
		patch["name"] = newName
	}
	if in.Description != nil {
		patch["description"] = strings.TrimSpace(*in.Description)
	}
	if newUnitID > 0 {
		patch["consumable_unit"] = newUnitID
	}
	if setType {
		patch["consumable_type_id"] = newTypeID
	}
	if in.StockMin != nil {
		patch["stock_min"] = *in.StockMin
	}
	if in.Enabled != nil {
		patch["enabled"] = *in.Enabled
	}
	if v := intOf(doc["_version"]); v > 0 {
		patch["_version"] = v
	}

	if err := s.sub.PatchEntity(ctx, consumableEntity, id, patch); err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return Consumable{}, rejection("stale_state", "the consumable changed while it was being updated: read it again and retry", "")
		}
		return Consumable{}, classify(err, "")
	}
	for k, v := range patch {
		if k != "_version" {
			doc[k] = v
		}
	}
	stock, err := s.loadConsumableStockMap(ctx, intOf(id), doc)
	if err != nil {
		return Consumable{}, err
	}
	per := make(map[string]int64, len(stock))
	for wid, units := range stock {
		per[strconv.FormatInt(wid, 10)] = units
	}
	types, err := s.labelsOf(ctx, consumableTypesEntity, false)
	if err != nil {
		return Consumable{}, err
	}
	units, err := s.labelsOf(ctx, unitEntity, true)
	if err != nil {
		return Consumable{}, err
	}
	v := inventoryView{stock: map[string]map[string]int64{id: per}}
	return v.consumable(withID(doc, id), types, units), nil
}

func normalizeGPS(raw string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	if len(parts) != 2 {
		return "", false
	}
	lat, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lng, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if e1 != nil || e2 != nil || math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lng) || math.IsInf(lng, 0) {
		return "", false
	}
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return "", false
	}
	return strconv.FormatFloat(lat, 'f', -1, 64) + "," + strconv.FormatFloat(lng, 'f', -1, 64), true
}

func (s *Service) validateConsumableRefs(ctx context.Context, unitID, typeID int64) (unitLabel, typeName string, problems []fieldProblem, err error) {
	if unitID > 0 {
		doc, gerr := s.sub.GetEntity(ctx, unitEntity, strconv.FormatInt(unitID, 10))
		if gerr != nil {
			if isMissing(gerr) {
				problems = append(problems, fieldProblem{"unit_id", fmt.Sprintf("unit %d does not exist", unitID)})
			} else {
				return "", "", nil, classify(gerr, "")
			}
		} else {
			sym, _ := doc["symbol"].(string)
			name, _ := doc["name"].(string)
			unitLabel = strings.TrimSpace(sym)
			if unitLabel == "" {
				unitLabel = strings.TrimSpace(name)
			}
		}
	}
	if typeID > 0 {
		doc, gerr := s.sub.GetEntity(ctx, consumableTypesEntity, strconv.FormatInt(typeID, 10))
		if gerr != nil {
			if isMissing(gerr) {
				problems = append(problems, fieldProblem{"consumable_type_id", fmt.Sprintf("consumable type %d does not exist", typeID)})
			} else {
				return "", "", nil, classify(gerr, "")
			}
		} else {
			typeName, _ = doc["name"].(string)
			typeName = strings.TrimSpace(typeName)
		}
	}
	return unitLabel, typeName, problems, nil
}
