package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Header phases: when the person fills an item of a template's header.
const (
	phaseCreating  = "upon_creating"
	phaseExecuting = "upon_executing"
)

// Assignment vocabulary (seam-CURE's create form).
const (
	AssignmentDirect = "direct_assignment"
	AssignmentBoard  = "board_posting"
)

// Entities a work order points at.
const (
	assetEntity       = "assets"
	crewEntity        = "crews"
	outsourcedEntity  = "outsourced"
	workflowModeTmpl  = "template"
	workflowRuleKey   = "rule"
	publisherSourceID = "work-orders-publisher"
)

type headerItem struct {
	Tag          string `json:"tag"`
	Type         string `json:"type"`
	DefaultValue string `json:"default_value"`
	ToFillOut    string `json:"to_fill_out"`
	Required     bool   `json:"required"`
}

// workOrderTemplate is the part of a work_order_templates document the
// publisher needs to open and advance an order.
type workOrderTemplate struct {
	ID          string
	Version     int
	Name        string
	Description string
	AssetTypeID int64
	MinAssets   int
	MaxAssets   int
	Headers     []headerItem
	States      []string

	// Blocks and the mobile warehouse are not supported yet: a template that
	// uses them cannot be opened here, or Seam would show an incomplete order.
	HasBlocks       bool
	UsesConsumables bool
}

func parseTemplateDoc(id string, doc map[string]any) (workOrderTemplate, error) {
	t := workOrderTemplate{ID: id}
	t.Name, _ = doc["name"].(string)
	t.Description, _ = doc["description"].(string)
	t.Version = int(intOf(doc["version"]))
	t.AssetTypeID = intOf(doc["asset_type_id"])
	t.MinAssets = int(intOf(doc["min_assets"]))
	t.MaxAssets = int(intOf(doc["max_assets"]))
	t.UsesConsumables, _ = doc["consumables_allowed"].(bool)

	var states []struct {
		Name string `json:"name"`
	}
	if err := roundTrip(doc["workflow_states"], &states); err != nil {
		return t, fmt.Errorf("decode workflow_states: %w", err)
	}
	for _, st := range states {
		t.States = append(t.States, strings.TrimSpace(st.Name))
	}
	if err := roundTrip(doc["header_items"], &t.Headers); err != nil {
		return t, fmt.Errorf("decode header_items: %w", err)
	}
	var blocks []json.RawMessage
	if err := roundTrip(doc["blocks"], &blocks); err != nil {
		return t, fmt.Errorf("decode blocks: %w", err)
	}
	t.HasBlocks = len(blocks) > 0
	return t, nil
}

// validateWorkflow mirrors seam-CURE: at least two states, distinct and named.
func (t workOrderTemplate) validateWorkflow() error {
	if len(t.States) < 2 {
		return errors.New("workflow requires at least two states")
	}
	seen := map[string]bool{}
	for _, name := range t.States {
		if name == "" || seen[name] {
			return errors.New("workflow states must have distinct nonempty names")
		}
		seen[name] = true
	}
	return nil
}

// fieldProblem is one invalid request field and why.
type fieldProblem struct{ Field, Msg string }

// validationFrom turns collected problems into one ValidationError.
func validationFrom(problems []fieldProblem) error {
	if len(problems) == 0 {
		return nil
	}
	seen := map[string]bool{}
	ve := &ValidationError{perField: true}
	var parts []string
	for _, p := range problems {
		if !seen[p.Field] {
			seen[p.Field] = true
			ve.Fields = append(ve.Fields, p.Field)
		}
		parts = append(parts, p.Field+": "+p.Msg)
	}
	ve.Reason = strings.Join(parts, "; ")
	return ve
}

// isMissing reports whether xolu said the record or its whole entity does not exist.
func isMissing(err error) bool {
	var xe *XoluError
	if !errors.As(err, &xe) {
		return false
	}
	return xe.Status == 404 || (xe.Code == "XOLU-QL004" && strings.Contains(xe.Message, "does not exist"))
}

func (s *Service) loadTemplate(ctx context.Context, id string) (workOrderTemplate, error) {
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n <= 0 {
		return workOrderTemplate{}, &ValidationError{Fields: []string{"template_id"}, Reason: "must be a positive id"}
	}
	doc, err := s.sub.GetEntity(ctx, TemplateEntityType, id)
	if err != nil {
		if isMissing(err) {
			return workOrderTemplate{}, &ValidationError{Fields: []string{"template_id"}, Reason: "template not found"}
		}
		return workOrderTemplate{}, classify(err, "")
	}
	tpl, err := parseTemplateDoc(id, doc)
	if err != nil {
		return workOrderTemplate{}, &ValidationError{Fields: []string{"template_id"}, Reason: "template is malformed: " + err.Error()}
	}
	return tpl, nil
}

// headerValues validates the answers for the header items of one phase.
//
// base holds the answers already stored (empty on creation). Only supplied
// items change; the rest keep their value. A required item with no value after
// the merge is a problem. When applyDefaults is set an item that was not
// supplied takes the template's default, as seam-CURE's form pre-fills it.
func (s *Service) headerValues(ctx context.Context, items []headerItem, phase string, supplied, base map[string]any, applyDefaults bool) (map[string]any, []fieldProblem, error) {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	var problems []fieldProblem

	inPhase := map[string]bool{}
	for _, it := range items {
		if it.ToFillOut == phase {
			inPhase[it.Tag] = true
		}
	}
	tags := make([]string, 0, len(supplied))
	for tag := range supplied {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	for _, tag := range tags {
		if !inPhase[tag] {
			problems = append(problems, fieldProblem{"header_values." + tag, "not a header item that is filled " + phase})
		}
	}

	for _, it := range items {
		if it.ToFillOut != phase {
			continue
		}
		field := "header_values." + it.Tag
		raw, given := supplied[it.Tag]
		if !given || isBlank(raw) {
			if !given && applyDefaults && it.DefaultValue != "" {
				raw, given = it.DefaultValue, true
			}
		}
		if !given || isBlank(raw) {
			_, have := out[it.Tag]
			switch {
			case have && !given:
				// keep the stored answer
			case it.Type == "boolean" && applyDefaults:
				out[it.Tag] = false // an unchecked box, as the form posts it
			case it.Required && it.Type != "boolean":
				problems = append(problems, fieldProblem{field, "required"})
			}
			continue
		}
		typed, msg, err := s.typedHeaderValue(ctx, it, raw)
		if err != nil {
			return nil, nil, err
		}
		if msg != "" {
			problems = append(problems, fieldProblem{field, msg})
			continue
		}
		out[it.Tag] = typed
	}
	return out, problems, nil
}

func isBlank(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// typedHeaderValue converts one answer to the type its header item declares,
// with the same rules as seam-CURE's execution form. msg is a validation
// message; err is a substrate failure.
func (s *Service) typedHeaderValue(ctx context.Context, it headerItem, raw any) (any, string, error) {
	switch strings.ToLower(strings.TrimSpace(it.Type)) {
	case "number":
		var f float64
		switch t := raw.(type) {
		case float64:
			f = t
		case json.Number:
			f, _ = t.Float64()
		case int:
			f = float64(t)
		case int64:
			f = float64(t)
		case string:
			var err error
			if f, err = strconv.ParseFloat(strings.TrimSpace(t), 64); err != nil {
				return nil, "must be a number", nil
			}
		default:
			return nil, "must be a number", nil
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, "must be a finite number", nil
		}
		return f, "", nil
	case "boolean":
		switch t := raw.(type) {
		case bool:
			return t, "", nil
		case string:
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "true", "1", "on":
				return true, "", nil
			case "false", "0", "off":
				return false, "", nil
			}
		}
		return nil, "must be true or false", nil
	case "date":
		str, _ := raw.(string)
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(str)); err != nil {
			return nil, "must be a date as YYYY-MM-DD", nil
		}
		return strings.TrimSpace(str), "", nil
	case "asset":
		id := idString(raw)
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || n <= 0 {
			return nil, "must be an asset id", nil
		}
		if _, err := s.sub.GetEntity(ctx, assetEntity, id); err != nil {
			if isMissing(err) {
				return nil, "asset " + id + " does not exist", nil
			}
			return nil, "", classify(err, "")
		}
		return n, "", nil
	case "gps":
		str, _ := raw.(string)
		parts := strings.Split(str, ",")
		if len(parts) == 2 {
			lat, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			lon, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if e1 == nil && e2 == nil && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 {
				return str, "", nil
			}
		}
		return nil, `must be "latitude,longitude"`, nil
	default: // text
		str, ok := raw.(string)
		if !ok {
			return nil, "must be text", nil
		}
		return str, "", nil
	}
}

// assetSelection validates the assets chosen for an order against the
// template's limits and asset type.
func (s *Service) assetSelection(ctx context.Context, tpl workOrderTemplate, ids []ID) ([]int64, []fieldProblem, error) {
	const field = "asset_ids"
	var problems []fieldProblem
	if tpl.MinAssets > 0 && len(ids) < tpl.MinAssets {
		problems = append(problems, fieldProblem{field, fmt.Sprintf("the template needs at least %d asset(s)", tpl.MinAssets)})
	}
	if tpl.MaxAssets > 0 && len(ids) > tpl.MaxAssets {
		problems = append(problems, fieldProblem{field, fmt.Sprintf("the template allows at most %d asset(s)", tpl.MaxAssets)})
	}
	if len(problems) > 0 {
		return nil, problems, nil
	}
	out := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, raw := range ids {
		n, err := strconv.ParseInt(string(raw), 10, 64)
		if err != nil || n <= 0 {
			return nil, []fieldProblem{{field, fmt.Sprintf("%q is not an asset id", string(raw))}}, nil
		}
		if seen[n] {
			return nil, []fieldProblem{{field, fmt.Sprintf("asset %d is repeated", n)}}, nil
		}
		doc, err := s.sub.GetEntity(ctx, assetEntity, string(raw))
		if err != nil {
			if isMissing(err) {
				return nil, []fieldProblem{{field, fmt.Sprintf("asset %d does not exist", n)}}, nil
			}
			return nil, nil, classify(err, "")
		}
		if tpl.AssetTypeID > 0 && intOf(doc["type_id"]) != tpl.AssetTypeID {
			return nil, []fieldProblem{{field, fmt.Sprintf("asset %d is not of the template's asset type", n)}}, nil
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil, nil
}

// assignment is who an order is for.
type assignment struct {
	Type, ResponsibleType string
	ResponsibleID         int64
	Name                  string
}

// resolveAssignment validates and resolves the assignment of an order.
//
// A direct assignment points at a crew or an outsourced party, whose name is
// read from xolu. Users live in Seam's own account store, which this publisher
// cannot read, so a user cannot be assigned directly here; a board posting
// (anyone of that responsible type may claim it) is fine for any type.
func (s *Service) resolveAssignment(ctx context.Context, in CreateInput) (assignment, []fieldProblem, error) {
	a := assignment{Type: strings.TrimSpace(in.AssignmentType), ResponsibleType: strings.TrimSpace(in.ResponsibleType)}
	var problems []fieldProblem
	switch a.Type {
	case AssignmentDirect, AssignmentBoard:
	case "":
		problems = append(problems, fieldProblem{"assignment_type", "required: direct_assignment or board_posting"})
	default:
		problems = append(problems, fieldProblem{"assignment_type", "must be direct_assignment or board_posting"})
	}
	switch a.ResponsibleType {
	case "user", "crew", "third_party":
	case "":
		problems = append(problems, fieldProblem{"responsible_type", "required: user, crew or third_party"})
	default:
		problems = append(problems, fieldProblem{"responsible_type", "must be user, crew or third_party"})
	}
	if len(problems) > 0 {
		return a, problems, nil
	}

	if a.Type == AssignmentBoard {
		if in.ResponsibleID != "" {
			return a, []fieldProblem{{"responsible_id", "a board posting has no responsible until someone claims it"}}, nil
		}
		return a, nil, nil
	}

	var entity string
	switch a.ResponsibleType {
	case "crew":
		entity = crewEntity
	case "third_party":
		entity = outsourcedEntity
	default:
		return a, []fieldProblem{{"responsible_type", "a user cannot be assigned directly from here (users are not in xolu); use a crew or third_party, or a board_posting"}}, nil
	}
	id, err := strconv.ParseInt(string(in.ResponsibleID), 10, 64)
	if err != nil || id <= 0 {
		return a, []fieldProblem{{"responsible_id", "required for a direct assignment"}}, nil
	}
	doc, err := s.sub.GetEntity(ctx, entity, string(in.ResponsibleID))
	if err != nil {
		if isMissing(err) {
			return a, []fieldProblem{{"responsible_id", fmt.Sprintf("%s %d does not exist", a.ResponsibleType, id)}}, nil
		}
		return a, nil, classify(err, "")
	}
	name, _ := doc["name"].(string)
	if strings.TrimSpace(name) == "" {
		return a, []fieldProblem{{"responsible_id", fmt.Sprintf("%s %d has no name", a.ResponsibleType, id)}}, nil
	}
	a.ResponsibleID, a.Name = id, strings.TrimSpace(name)
	return a, nil, nil
}

// rejection builds the typed error for a rule of the template workflow. It
// uses the INVALID_TRANSITION envelope, but the rule is the publisher's own,
// not an xolu FSM: details.source says so.
func rejection(rule, message, state string) *TransitionError {
	d := map[string]any{workflowRuleKey: rule, "source": publisherSourceID}
	if state != "" {
		d["state"] = state
	}
	return &TransitionError{Code: "XOLU-INVALID_TRANSITION", Message: message, Details: d}
}

// intOf reads an integer from any JSON-ish value; anything else is 0.
func intOf(v any) int64 {
	n, err := strconv.ParseInt(idString(v), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// roundTrip decodes a loosely typed JSON value into dst. nil leaves dst alone.
func roundTrip(src any, dst any) error {
	if src == nil {
		return nil
	}
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
