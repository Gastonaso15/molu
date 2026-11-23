package orders

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const assetTypesEntity = "asset_types"

// CreateTemplateInput is the payload of work_orders.CreateTemplate.
type CreateTemplateInput struct {
	Name                    string               `json:"name"`
	Description             string               `json:"description,omitempty"`
	Color                   string               `json:"color,omitempty"`
	Icon                    string               `json:"icon,omitempty"`
	AssetTypeID             ID                   `json:"asset_type_id,omitempty"`
	MinAssets               int                  `json:"min_assets,omitempty"`
	MaxAssets               int                  `json:"max_assets,omitempty"`
	WorkflowStates          []string             `json:"workflow_states"`
	HeaderItems             []TemplateHeaderItem `json:"header_items,omitempty"`
	Blocks                  []TemplateBlock      `json:"blocks,omitempty"`
	ConsumablesEnabled      bool                 `json:"consumables_enabled,omitempty"`
	ExtraConsumablesAllowed bool                 `json:"extra_consumables_allowed,omitempty"`
	Consumables             []TemplateConsumable `json:"consumables,omitempty"`
	Actions                 []TemplateAction     `json:"actions,omitempty"`
	Rules                   []map[string]any     `json:"rules,omitempty"`
	Numbering               *Numbering           `json:"numbering,omitempty"`
}

// PublishTemplateVersionInput is the payload of
// work_orders.PublishTemplateVersion. Templates are never mutated in place so
// existing orders retain their version: PublishTemplateVersion atomically
// marks PreviousTemplateID as latest=false (guarded by _version) and appends a
// new template record with version+1 and latest=true.
type PublishTemplateVersionInput struct {
	PreviousTemplateID ID `json:"previous_template_id"`
	CreateTemplateInput
}

// CreateTemplate validates the full template structure and saves version 1
// with latest=true in work_order_templates.
func (s *Service) CreateTemplate(ctx context.Context, in CreateTemplateInput) (Template, error) {
	doc, consNames, actNames, err := s.buildTemplateDoc(ctx, in, 1)
	if err != nil {
		return Template{}, err
	}
	id, err := s.sub.CreateEntity(ctx, TemplateEntityType, doc)
	if err != nil {
		return Template{}, classify(err, "")
	}
	tpl := templateFromRow(withID(doc, id))
	applyTemplateNames(&tpl, consNames, actNames)
	return tpl, nil
}

// PublishTemplateVersion supersedes PreviousTemplateID (which must be latest)
// in one atomic Commit: PreviousTemplateID gets latest=false (guarded by its
// _version) and a new work_order_templates document is appended with version+1
// and latest=true.
func (s *Service) PublishTemplateVersion(ctx context.Context, in PublishTemplateVersionInput) (Template, error) {
	prevID, ok := parsePositiveID(in.PreviousTemplateID)
	if !ok {
		return Template{}, &ValidationError{Fields: []string{"previous_template_id"}, Reason: "must be a positive id"}
	}
	prevIDStr := strconv.FormatInt(prevID, 10)
	prevDoc, err := s.sub.GetEntity(ctx, TemplateEntityType, prevIDStr)
	if err != nil {
		if isMissing(err) {
			return Template{}, &NotFoundError{ID: prevIDStr, Kind: "work order template"}
		}
		return Template{}, classify(err, "")
	}
	if latest, _ := prevDoc["latest"].(bool); !latest {
		return Template{}, rejection("not_latest", "only the latest version of a template can be superseded by a new version", "")
	}
	prevVersion := int(intOf(prevDoc["_version"]))
	if prevVersion <= 0 || int64(prevVersion) != intOf(prevDoc["_version"]) {
		return Template{}, fmt.Errorf("work order template %s has no valid _version: cannot version it safely", prevIDStr)
	}

	nextVersion := int(intOf(prevDoc["version"])) + 1
	if nextVersion < 2 {
		nextVersion = 2
	}
	newDoc, consNames, actNames, err := s.buildTemplateDoc(ctx, in.CreateTemplateInput, nextVersion)
	if err != nil {
		return Template{}, err
	}
	newID, err := s.newFreeEntityID(ctx, TemplateEntityType)
	if err != nil {
		return Template{}, err
	}

	updatedPrev := make(map[string]any, len(prevDoc))
	for k, v := range prevDoc {
		if k != "_version" && k != "id" {
			updatedPrev[k] = v
		}
	}
	updatedPrev["latest"] = false

	appendID := newID
	commitErr := s.sub.Commit(ctx, CommitRequest{
		Update: CommitUpdate{
			Entity:  TemplateEntityType,
			ID:      prevID,
			Version: &prevVersion,
			Data:    updatedPrev,
		},
		Append: []CommitAppend{
			{Entity: TemplateEntityType, ID: &appendID, Data: newDoc},
		},
	})
	if commitErr != nil {
		var xe *XoluError
		if errors.As(commitErr, &xe) && xe.Status == 409 {
			return Template{}, rejection("stale_state", "the previous template version changed concurrently: read it again and retry", "")
		}
		return Template{}, classify(commitErr, "")
	}

	newIDStr := strconv.FormatInt(newID, 10)
	tpl := templateFromRow(withID(newDoc, newIDStr))
	applyTemplateNames(&tpl, consNames, actNames)
	return tpl, nil
}

func applyTemplateNames(tpl *Template, consNames, actNames map[string]string) {
	for i := range tpl.Consumables {
		tpl.Consumables[i].Name = consNames[tpl.Consumables[i].ConsumableID]
	}
	for i := range tpl.Actions {
		tpl.Actions[i].Name = actNames[tpl.Actions[i].ActionID]
	}
}

func (s *Service) buildTemplateDoc(ctx context.Context, in CreateTemplateInput, version int) (map[string]any, map[string]string, map[string]string, error) {
	var problems []fieldProblem
	name := strings.TrimSpace(in.Name)
	if name == "" {
		problems = append(problems, fieldProblem{"name", "required"})
	} else if utf8.RuneCountInString(name) > maxCatalogNameRunes {
		problems = append(problems, fieldProblem{"name", fmt.Sprintf("must be at most %d characters", maxCatalogNameRunes)})
	}

	// Workflow states: at least 2 distinct non-empty states.
	states := make([]map[string]any, 0, len(in.WorkflowStates))
	seenStates := map[string]bool{}
	if len(in.WorkflowStates) < 2 {
		problems = append(problems, fieldProblem{"workflow_states", "at least two workflow states are required"})
	} else {
		for _, raw := range in.WorkflowStates {
			st := strings.TrimSpace(raw)
			if st == "" {
				problems = append(problems, fieldProblem{"workflow_states", "workflow states must be non-empty"})
				break
			}
			if seenStates[st] {
				problems = append(problems, fieldProblem{"workflow_states", fmt.Sprintf("duplicate workflow state %q", st)})
				break
			}
			seenStates[st] = true
			states = append(states, map[string]any{"name": st})
		}
	}

	// Asset limits & type.
	if in.MinAssets < 0 {
		problems = append(problems, fieldProblem{"min_assets", "must be non-negative"})
	}
	if in.MaxAssets < 0 {
		problems = append(problems, fieldProblem{"max_assets", "must be non-negative"})
	} else if in.MaxAssets > 0 && in.MinAssets > in.MaxAssets {
		problems = append(problems, fieldProblem{"max_assets", "must be greater than or equal to min_assets"})
	}
	var assetTypeID int64
	if strings.TrimSpace(string(in.AssetTypeID)) != "" {
		id, ok := parsePositiveID(in.AssetTypeID)
		if !ok {
			problems = append(problems, fieldProblem{"asset_type_id", "must be a positive id"})
		} else {
			assetTypeID = id
		}
	}

	// Header items.
	headerDocs := make([]map[string]any, 0, len(in.HeaderItems))
	seenTags := map[string]bool{}
	for i, h := range in.HeaderItems {
		tag := strings.TrimSpace(h.Tag)
		if tag == "" {
			problems = append(problems, fieldProblem{"header_items", fmt.Sprintf("item %d has an empty tag", i)})
			continue
		}
		key := strings.ToLower(tag)
		if seenTags[key] {
			problems = append(problems, fieldProblem{"header_items", fmt.Sprintf("duplicate header tag %q", tag)})
			continue
		}
		seenTags[key] = true
		htype := strings.ToLower(strings.TrimSpace(h.Type))
		if !validTemplateFieldType(htype) {
			problems = append(problems, fieldProblem{"header_items", fmt.Sprintf("item %q has invalid type %q", tag, h.Type)})
		}
		filledAt := strings.TrimSpace(h.FilledAt)
		if filledAt != phaseCreating && filledAt != phaseExecuting {
			problems = append(problems, fieldProblem{"header_items", fmt.Sprintf("item %q must have filled_at %q or %q", tag, phaseCreating, phaseExecuting)})
		}
		headerDocs = append(headerDocs, map[string]any{
			"tag":           tag,
			"type":          htype,
			"default_value": h.DefaultValue,
			"to_fill_out":   filledAt,
			"required":      h.Required,
		})
	}

	// Blocks: DAG validation + responsible_type + line types + commit capacity.
	blockDocs := make([]map[string]any, 0, len(in.Blocks))
	blockNames := map[string]bool{}
	if len(in.Blocks) > 0 {
		if _, err := blockPredecessors(in.Blocks); err != nil {
			problems = append(problems, fieldProblem{"blocks", err.Error()})
		}
		for i, b := range in.Blocks {
			bname := strings.TrimSpace(b.Name)
			if bname != "" {
				blockNames[strings.ToLower(bname)] = true
			}
			rt := strings.TrimSpace(b.ResponsibleType)
			if rt != "user" && rt != "crew" && rt != "third_party" {
				problems = append(problems, fieldProblem{"blocks", fmt.Sprintf("block %d (%q) must have responsible_type user, crew or third_party", i, bname)})
			}
			seenLines := map[string]bool{}
			lineDocs := make([]map[string]any, 0, len(b.Lines))
			for j, ln := range b.Lines {
				lname := strings.TrimSpace(ln.Name)
				if lname == "" {
					problems = append(problems, fieldProblem{"blocks", fmt.Sprintf("block %q line %d has an empty name", bname, j)})
					continue
				}
				lkey := strings.ToLower(lname)
				if seenLines[lkey] {
					problems = append(problems, fieldProblem{"blocks", fmt.Sprintf("block %q has duplicate line %q", bname, lname)})
					continue
				}
				seenLines[lkey] = true
				ltype := strings.ToLower(strings.TrimSpace(ln.Type))
				if !validTemplateFieldType(ltype) {
					problems = append(problems, fieldProblem{"blocks", fmt.Sprintf("block %q line %q has invalid type %q", bname, lname, ln.Type)})
				}
				lineDocs = append(lineDocs, map[string]any{"name": lname, "type": ltype})
			}
			blockDocs = append(blockDocs, map[string]any{
				"name":             bname,
				"responsible_type": rt,
				"predecessor":      strings.TrimSpace(b.Predecessor),
				"lines":            lineDocs,
			})
		}
	}
	appendCount := len(in.Blocks)
	if in.ConsumablesEnabled {
		appendCount++
	}
	if appendCount > maxCommitAppends {
		problems = append(problems, fieldProblem{"blocks", fmt.Sprintf("template needs %d commit records (blocks + mobile warehouse), maximum is %d", appendCount, maxCommitAppends)})
	}

	// Consumables.
	consDocs := make([]map[string]any, 0, len(in.Consumables))
	var consIDs []int64
	if !in.ConsumablesEnabled {
		if in.ExtraConsumablesAllowed || len(in.Consumables) > 0 {
			problems = append(problems, fieldProblem{"consumables", "consumables_enabled must be true to configure consumables or extra consumables"})
		}
	} else {
		seenCons := map[int64]bool{}
		for i, c := range in.Consumables {
			cid, ok := parsePositiveID(ID(c.ConsumableID))
			if !ok {
				problems = append(problems, fieldProblem{"consumables", fmt.Sprintf("entry %d has invalid consumable_id %q", i, c.ConsumableID)})
				continue
			}
			if seenCons[cid] {
				problems = append(problems, fieldProblem{"consumables", fmt.Sprintf("duplicate consumable_id %d", cid)})
				continue
			}
			if math.IsNaN(c.Quantity) || math.IsInf(c.Quantity, 0) || c.Quantity <= 0 {
				problems = append(problems, fieldProblem{"consumables", fmt.Sprintf("consumable %d quantity must be greater than zero", cid)})
				continue
			}
			seenCons[cid] = true
			consIDs = append(consIDs, cid)
			consDocs = append(consDocs, map[string]any{"consumable_id": cid, "quantity": c.Quantity})
		}
	}

	// Actions.
	actDocs := make([]map[string]any, 0, len(in.Actions))
	var actIDs []int64
	for i, a := range in.Actions {
		aid, ok := parsePositiveID(ID(a.ActionID))
		if !ok {
			problems = append(problems, fieldProblem{"actions", fmt.Sprintf("entry %d has invalid action_id %q", i, a.ActionID)})
			continue
		}
		tp := strings.TrimSpace(a.TriggerPoint)
		if tp == "" {
			problems = append(problems, fieldProblem{"actions", fmt.Sprintf("action %d requires trigger_point", aid)})
		}
		bref := strings.TrimSpace(a.BlockReference)
		if bref != "" && !blockNames[strings.ToLower(bref)] {
			problems = append(problems, fieldProblem{"actions", fmt.Sprintf("action %d references unknown block %q", aid, bref)})
		}
		actIDs = append(actIDs, aid)
		mappings := a.MappingValues
		if mappings == nil {
			mappings = []ActionMapping{}
		}
		actDocs = append(actDocs, map[string]any{
			"action":          aid,
			"trigger_point":   tp,
			"block_reference": bref,
			"mapping_values":  mappings,
		})
	}

	// Numbering.
	var prefix, suffix, resetPolicy string
	var seqLen int
	if in.Numbering != nil {
		prefix = strings.TrimSpace(in.Numbering.Prefix)
		suffix = strings.TrimSpace(in.Numbering.Suffix)
		seqLen = in.Numbering.SequenceLength
		resetPolicy = strings.TrimSpace(in.Numbering.ResetPolicy)
		if seqLen < 0 {
			problems = append(problems, fieldProblem{"numbering", "sequence_length must be non-negative"})
		}
		switch resetPolicy {
		case "", "never", "yearly", "monthly", "daily":
		default:
			problems = append(problems, fieldProblem{"numbering", "reset_policy must be never, yearly, monthly or daily"})
		}
	}

	if err := validationFrom(problems); err != nil {
		return nil, nil, nil, err
	}

	// Validate external references in xolu.
	var refProblems []fieldProblem
	if assetTypeID > 0 {
		if _, err := s.sub.GetEntity(ctx, assetTypesEntity, strconv.FormatInt(assetTypeID, 10)); err != nil {
			if isMissing(err) {
				refProblems = append(refProblems, fieldProblem{"asset_type_id", fmt.Sprintf("asset type %d does not exist", assetTypeID)})
			} else {
				return nil, nil, nil, classify(err, "")
			}
		}
	}
	consNames := map[string]string{}
	for _, cid := range consIDs {
		idStr := strconv.FormatInt(cid, 10)
		d, err := s.sub.GetEntity(ctx, consumableEntity, idStr)
		if err != nil {
			if isMissing(err) {
				refProblems = append(refProblems, fieldProblem{"consumables", fmt.Sprintf("consumable %d does not exist", cid)})
			} else {
				return nil, nil, nil, classify(err, "")
			}
			continue
		}
		consNames[idStr], _ = d["name"].(string)
	}
	actNames := map[string]string{}
	for _, aid := range actIDs {
		idStr := strconv.FormatInt(aid, 10)
		d, err := s.sub.GetEntity(ctx, woActionEntity, idStr)
		if err != nil {
			if isMissing(err) {
				refProblems = append(refProblems, fieldProblem{"actions", fmt.Sprintf("action %d does not exist", aid)})
			} else {
				return nil, nil, nil, classify(err, "")
			}
			continue
		}
		actNames[idStr], _ = d["name"].(string)
	}
	if err := validationFrom(refProblems); err != nil {
		return nil, nil, nil, err
	}

	rules := in.Rules
	if rules == nil {
		rules = []map[string]any{}
	}
	doc := map[string]any{
		"version":                     version,
		"latest":                      true,
		"name":                        name,
		"description":                 strings.TrimSpace(in.Description),
		"color":                       strings.TrimSpace(in.Color),
		"icon":                        strings.TrimSpace(in.Icon),
		"asset_type_id":               assetTypeID,
		"min_assets":                  in.MinAssets,
		"max_assets":                  in.MaxAssets,
		"header_items":                headerDocs,
		"blocks":                      blockDocs,
		"workflow_states":             states,
		"actions":                     actDocs,
		"prefix":                      prefix,
		"sequence_length":             seqLen,
		"suffix":                      suffix,
		"reset_policy":                resetPolicy,
		"rule_item":                   rules,
		"consumables_allowed":         in.ConsumablesEnabled,
		"add_any_consumables_allowed": in.ExtraConsumablesAllowed,
		"consumables":                 consDocs,
	}
	return doc, consNames, actNames, nil
}

func validTemplateFieldType(t string) bool {
	switch t {
	case "text", "number", "boolean", "date", "asset", "gps":
		return true
	}
	return false
}

func (s *Service) newFreeEntityID(ctx context.Context, entity string) (int64, error) {
	for attempt := 0; attempt < 5; attempt++ {
		var raw [8]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, fmt.Errorf("generate %s id: %w", entity, err)
		}
		id := int64(binary.BigEndian.Uint64(raw[:])&((1<<52)-1)) + 1
		_, err := s.sub.GetEntity(ctx, entity, strconv.FormatInt(id, 10))
		switch {
		case err == nil:
			continue
		case isMissing(err):
			return id, nil
		default:
			return 0, classify(err, "")
		}
	}
	return 0, fmt.Errorf("could not find a free %s id", entity)
}
