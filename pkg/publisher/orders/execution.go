package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Block lifecycle states (the shared block definition's states).
const (
	blockPending    = "pending"
	blockInProgress = "in_progress"
	blockCompleted  = "completed"
)

// Block intents of ExecuteBlock.
const (
	IntentStart    = "start"
	IntentSave     = "save"
	IntentComplete = "complete"
)

// executionOrder reports whether a stored order runs under the execution
// model with its own workflow machine and block records: a template order
// with block_execution_version 1 and a machine. Orders opened before that
// (no machine) keep the older single-write advance.
func executionOrder(doc map[string]any) bool {
	return templateOrder(doc) && intOf(doc["block_execution_version"]) == 1 && intOf(doc["machine_id"]) > 0
}

// execution is the snapshot the execution tools work on.
type execution struct {
	id          string
	doc         map[string]any
	tpl         workOrderTemplate
	idx, last   int
	state       string
	preds       []int
	blocks      []map[string]any // by block_index, each with its "id"
	headerReady bool
	open        bool
}

// loadExecution reads an order, its template and its block records, and
// checks they agree, as the work order module does before it lets anyone act.
func (s *Service) loadExecution(ctx context.Context, orderID string) (*execution, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, &ValidationError{Fields: []string{"order_id"}}
	}
	doc, err := s.sub.GetEntity(ctx, EntityType, orderID)
	if err != nil {
		return nil, classify(err, orderID)
	}
	state, _ := doc["state"].(string)
	if !executionOrder(doc) {
		return nil, rejection("unsupported_execution_model",
			"this order does not use block execution (it is not a template order with its own workflow machine and block_execution_version 1)", state)
	}
	tpl, err := s.loadTemplate(ctx, idString(doc["template_id"]))
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return nil, rejection("template_unavailable", "the order's template cannot be used: "+ve.Reason, state)
		}
		return nil, err
	}
	if v := int(intOf(doc["template_version"])); v > 0 && v != tpl.Version {
		return nil, rejection("template_version", fmt.Sprintf("the order was opened with template version %d, which is not the stored one (%d)", v, tpl.Version), state)
	}
	if err := tpl.validateWorkflow(); err != nil {
		return nil, rejection("invalid_workflow", "the template workflow is invalid: "+err.Error(), state)
	}
	idx := int(intOf(doc["workflow_index"]))
	if idx < 0 || idx >= len(tpl.States) || tpl.States[idx] != state {
		return nil, rejection("invalid_workflow", fmt.Sprintf("the order's state %q does not match its position in the template workflow", state), state)
	}
	preds, err := blockPredecessors(tpl.Blocks)
	if err != nil {
		return nil, rejection("invalid_workflow", "the template blocks are invalid: "+err.Error(), state)
	}
	x := &execution{
		id: orderID, doc: doc, tpl: tpl, idx: idx, last: len(tpl.States) - 1, state: state, preds: preds,
		headerReady: strings.TrimSpace(fmt.Sprint(valueOr(doc["execution_header_completed_at"], ""))) != "",
		open:        !(doc["assignment_type"] == AssignmentBoard && intOf(doc["responsible_id"]) == 0) && idx < len(tpl.States)-1,
		blocks:      make([]map[string]any, len(tpl.Blocks)),
	}
	if len(tpl.Blocks) == 0 {
		return x, nil
	}
	rows, err := s.sub.FindEntities(ctx, workOrderBlocksEntity, EntityQuery{
		Where: []Condition{{Field: "work_order_id", Op: "=", Value: numericOrString(orderID)}},
		Limit: maxBlocksPerOrder, OrderBy: []string{"id"},
	})
	if err != nil && !isMissing(err) {
		return nil, classify(err, orderID)
	}
	if len(rows) != len(tpl.Blocks) {
		return nil, rejection("inconsistent_blocks", fmt.Sprintf("the order has %d block records but its template has %d blocks", len(rows), len(tpl.Blocks)), state)
	}
	for _, r := range rows {
		bi := int(intOf(r["block_index"]))
		if bi < 0 || bi >= len(x.blocks) || x.blocks[bi] != nil {
			return nil, rejection("inconsistent_blocks", "a block record has an invalid or repeated block_index", state)
		}
		if int(intOf(r["predecessor_index"])) != preds[bi] || strings.TrimSpace(fmt.Sprint(valueOr(r["responsible_type"], ""))) != strings.TrimSpace(tpl.Blocks[bi].ResponsibleType) {
			return nil, rejection("inconsistent_blocks", fmt.Sprintf("block %d does not match its template", bi), state)
		}
		x.blocks[bi] = r
	}
	return x, nil
}

func valueOr(v any, def string) any {
	if v == nil {
		return def
	}
	return v
}

// predecessorCompleted evaluates each block on its own, so branches can run in parallel.
func (x *execution) predecessorCompleted(i int) bool {
	p := x.preds[i]
	return p < 0 || x.blocks[p]["state"] == blockCompleted
}

// actsFor reports whether a user is the person an assignment points at: the
// user itself, the responsible (or a member) of a crew, or the responsible of
// a third party. There is no bypass by role.
func (s *Service) actsFor(ctx context.Context, respType string, respID, userID int64) (bool, error) {
	if respID <= 0 || userID <= 0 {
		return false, nil
	}
	switch respType {
	case "user":
		return respID == userID, nil
	case "crew", "third_party":
		entity := crewEntity
		if respType == "third_party" {
			entity = outsourcedEntity
		}
		doc, err := s.sub.GetEntity(ctx, entity, fmt.Sprint(respID))
		if err != nil {
			if isMissing(err) {
				return false, nil
			}
			return false, classify(err, "")
		}
		if intOf(doc["responsible_user_id"]) == userID {
			return true, nil
		}
		if respType == "crew" {
			for _, m := range idList(doc["members"]) {
				if intOf(m) == userID {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// actingUser parses the id of the user who acts. The publisher has no session
// of its own: the caller says who acts (see ClaimOrder).
func actingUser(id ID) (int64, error) {
	n, ok := parsePositiveID(id)
	if !ok {
		return 0, &ValidationError{Fields: []string{"user_id"}, Reason: "required: the id of the user who acts"}
	}
	return n, nil
}

// requireOwner refuses anyone who is not the order's responsible.
func (s *Service) requireOwner(ctx context.Context, x *execution, userID int64) error {
	ok, err := s.actsFor(ctx, fmt.Sprint(x.doc["responsible_type"]), intOf(x.doc["responsible_id"]), userID)
	if err != nil {
		return err
	}
	if !ok {
		return rejection("not_responsible", "only the order's responsible can do this", x.state)
	}
	return nil
}

func staleOrConflict(err error, state, what string) error {
	var xe *XoluError
	if errors.As(err, &xe) && xe.Status == 409 {
		return rejection("stale_state", what+" changed concurrently: read it again and retry", state)
	}
	return nil
}

// ConfirmHeaderInput is the payload of work_orders.ConfirmExecutionHeader.
type ConfirmHeaderInput struct {
	OrderID string `json:"order_id"`
	// UserID is the user who confirms; it must be the order's responsible.
	UserID ID `json:"user_id"`
	// HeaderValues answers the header items filled "upon_executing", by tag.
	HeaderValues map[string]any `json:"header_values,omitempty"`
}

// ConfirmHeaderResult is the order after its header was confirmed.
type ConfirmHeaderResult struct {
	Order       WorkOrder `json:"order"`
	ConfirmedAt string    `json:"confirmed_at"`
}

// ConfirmExecutionHeader confirms the header items that are filled at
// execution time and freezes them. Until then no block can start and the
// order cannot advance. Only the order's responsible can do it, once, while
// the order is open (claimed, and not in its final state).
func (s *Service) ConfirmExecutionHeader(ctx context.Context, in ConfirmHeaderInput) (ConfirmHeaderResult, error) {
	userID, err := actingUser(in.UserID)
	if err != nil {
		return ConfirmHeaderResult{}, err
	}
	x, err := s.loadExecution(ctx, in.OrderID)
	if err != nil {
		return ConfirmHeaderResult{}, err
	}
	if !x.open {
		return ConfirmHeaderResult{}, closedRejection(x)
	}
	if err := s.requireOwner(ctx, x, userID); err != nil {
		return ConfirmHeaderResult{}, err
	}
	if x.headerReady {
		return ConfirmHeaderResult{}, rejection("header_already_confirmed", "the execution header was already confirmed and is frozen", x.state)
	}
	var base map[string]any
	if err := roundTrip(x.doc["header_values"], &base); err != nil {
		base = nil
	}
	headers, problems, err := s.headerValues(ctx, x.tpl.Headers, phaseExecuting, in.HeaderValues, base, false)
	if err != nil {
		return ConfirmHeaderResult{}, err
	}
	if err := validationFrom(problems); err != nil {
		return ConfirmHeaderResult{}, err
	}
	version := intOf(x.doc["_version"])
	if version <= 0 {
		return ConfirmHeaderResult{}, fmt.Errorf("work order %s has no _version: cannot change it safely", x.id)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]any{
		"header_values":                         headers,
		"execution_header_completed_at":         now,
		"execution_header_completed_by_user_id": userID,
		"updated_at":                            now,
		"_version":                              version,
	}
	if err := s.sub.PatchEntity(ctx, EntityType, x.id, patch); err != nil {
		if r := staleOrConflict(err, x.state, "the order"); r != nil {
			return ConfirmHeaderResult{}, r
		}
		return ConfirmHeaderResult{}, classify(err, x.id)
	}
	for k, v := range patch {
		if k != "_version" {
			x.doc[k] = v
		}
	}
	return ConfirmHeaderResult{Order: orderFromDoc(x.id, x.doc), ConfirmedAt: now}, nil
}

func closedRejection(x *execution) error {
	if x.idx == x.last {
		return rejection("final_state", fmt.Sprintf("the order is already in its final state %q", x.state), x.state)
	}
	return rejection("awaiting_claim", "the order is on the board and nobody has claimed it yet", x.state)
}

// ExecuteBlockInput is the payload of work_orders.ExecuteBlock.
type ExecuteBlockInput struct {
	OrderID string `json:"order_id"`
	// Block is the block's name (case and surrounding spaces do not matter).
	Block string `json:"block"`
	// Intent is start, save or complete.
	Intent string `json:"intent"`
	// UserID is the user who acts; it must be the order's or the block's responsible.
	UserID ID `json:"user_id"`
	// Values answers the block's lines by line name. Optional: an answer that
	// is not given keeps the one already saved.
	Values map[string]any `json:"values,omitempty"`
}

// ExecuteBlockResult is the block after the intent.
type ExecuteBlockResult struct {
	Block    OrderBlock `json:"block"`
	Previous string     `json:"previous"`
	Order    WorkOrder  `json:"order"`
}

// ExecuteBlock starts, saves or completes one block. Starting needs the
// header confirmed and the block's predecessor completed; start and complete
// move the block's own machine in the same atomic write as the block record.
// The actor must be the order's responsible or the block's responsible.
func (s *Service) ExecuteBlock(ctx context.Context, in ExecuteBlockInput) (ExecuteBlockResult, error) {
	userID, err := actingUser(in.UserID)
	if err != nil {
		return ExecuteBlockResult{}, err
	}
	intent := strings.TrimSpace(in.Intent)
	if intent != IntentStart && intent != IntentSave && intent != IntentComplete {
		return ExecuteBlockResult{}, &ValidationError{Fields: []string{"intent"}, Reason: "must be start, save or complete"}
	}
	x, err := s.loadExecution(ctx, in.OrderID)
	if err != nil {
		return ExecuteBlockResult{}, err
	}
	bi := -1
	for i, b := range x.tpl.Blocks {
		if normName(b.Name) == normName(in.Block) {
			bi = i
		}
	}
	if bi < 0 {
		return ExecuteBlockResult{}, &ValidationError{Fields: []string{"block"}, Reason: "not a block of this order's template"}
	}
	block := x.blocks[bi]
	editor, err := s.actsFor(ctx, fmt.Sprint(x.doc["responsible_type"]), intOf(x.doc["responsible_id"]), userID)
	if err != nil {
		return ExecuteBlockResult{}, err
	}
	if !editor {
		if editor, err = s.actsFor(ctx, fmt.Sprint(block["responsible_type"]), intOf(block["responsible_id"]), userID); err != nil {
			return ExecuteBlockResult{}, err
		}
	}
	if !editor {
		return ExecuteBlockResult{}, rejection("not_responsible", "only the order's responsible or the block's responsible can run this block", x.state)
	}
	if !x.open {
		return ExecuteBlockResult{}, closedRejection(x)
	}
	if !x.headerReady {
		return ExecuteBlockResult{}, rejection("header_not_confirmed", "confirm the execution header (ConfirmExecutionHeader) before running blocks", x.state)
	}
	if !x.predecessorCompleted(bi) {
		return ExecuteBlockResult{}, rejection("predecessor_pending", fmt.Sprintf("block %q waits for block %q to be completed", strings.TrimSpace(x.tpl.Blocks[bi].Name), strings.TrimSpace(x.tpl.Blocks[x.preds[bi]].Name)), x.state)
	}
	prev, _ := block["state"].(string)
	want := blockPending
	if intent != IntentStart {
		want = blockInProgress
	}
	if prev != want {
		return ExecuteBlockResult{}, rejection("invalid_block_state", fmt.Sprintf("cannot %s a block that is %s", intent, prev), x.state)
	}
	blockID := intOf(block["id"])
	version := intOf(block["_version"])
	if blockID <= 0 || version <= 0 {
		return ExecuteBlockResult{}, fmt.Errorf("block %d of order %s has no id or _version: cannot change it safely", bi, x.id)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	merged := make(map[string]any, len(block)+6)
	for k, v := range block {
		if k != "_version" && k != "id" {
			merged[k] = v
		}
	}
	merged["updated_at"] = now
	merged["updated_by_user_id"] = userID

	if intent != IntentStart {
		var stored map[string]any
		if err := roundTrip(block["values"], &stored); err != nil {
			stored = nil
		}
		var supplied map[string]any
		if len(in.Values) > 0 {
			supplied = map[string]any{strings.TrimSpace(x.tpl.Blocks[bi].Name): in.Values}
		}
		answers, problems, err := s.blockAnswers(ctx, x.tpl, supplied, nil)
		if err != nil {
			return ExecuteBlockResult{}, err
		}
		for i := range problems {
			problems[i].Field = "values" + strings.TrimPrefix(problems[i].Field, "block_values."+strings.TrimSpace(x.tpl.Blocks[bi].Name))
		}
		if err := validationFrom(problems); err != nil {
			return ExecuteBlockResult{}, err
		}
		values := map[string]any{}
		for k, v := range stored {
			values[k] = v
		}
		for k, v := range answers {
			values[k] = v
		}
		merged["values"] = values
	}

	machine := intOf(block["machine_id"])
	switch intent {
	case IntentStart:
		merged["state"], merged["started_at"], merged["started_by_user_id"] = blockInProgress, now, userID
		err = s.commitBlock(ctx, x, blockID, version, machine, InputStart, blockInProgress, merged, bi, userID)
	case IntentComplete:
		merged["state"], merged["completed_at"], merged["completed_by_user_id"] = blockCompleted, now, userID
		err = s.commitBlock(ctx, x, blockID, version, machine, InputComplete, blockCompleted, merged, bi, userID)
	default: // save: values only, the machine does not move
		merged["_version"] = version
		err = s.sub.PatchEntity(ctx, workOrderBlocksEntity, fmt.Sprint(blockID), merged)
		if err != nil {
			if r := staleOrConflict(err, x.state, "the block"); r != nil {
				return ExecuteBlockResult{}, r
			}
			return ExecuteBlockResult{}, classify(err, x.id)
		}
		delete(merged, "_version")
	}
	if err != nil {
		return ExecuteBlockResult{}, err
	}
	out := OrderBlock{ID: fmt.Sprint(blockID), Index: bi}
	out.Name, _ = merged["name"].(string)
	out.State, _ = merged["state"].(string)
	out.StartedAt, _ = merged["started_at"].(string)
	out.CompletedAt, _ = merged["completed_at"].(string)
	if v, ok := merged["values"].(map[string]any); ok && len(v) > 0 {
		out.Values = v
	}
	if p := x.preds[bi]; p >= 0 {
		out.PredecessorIndex = &p
	}
	return ExecuteBlockResult{Block: out, Previous: prev, Order: orderFromDoc(x.id, x.doc)}, nil
}

// commitBlock saves the block record and moves its machine in one Commit, and
// checks the machine reached the state the record says.
func (s *Service) commitBlock(ctx context.Context, x *execution, blockID, version, machine int64, input, wantState string, data map[string]any, bi int, userID int64) error {
	v := int(version)
	got, err := s.sub.CommitWalk(ctx, CommitRequest{
		Update: CommitUpdate{Entity: workOrderBlocksEntity, ID: blockID, Version: &v, Data: data},
		FSMWalk: &CommitFSMWalk{Machine: machine, Input: input, Payload: map[string]any{
			"user_id": userID, "work_order_id": numericOrString(x.id), "block_index": bi,
		}},
	})
	if err != nil {
		if r := staleOrConflict(err, x.state, "the block"); r != nil {
			return r
		}
		return classify(err, x.id)
	}
	if got != wantState {
		return fmt.Errorf("the block machine reached %q, expected %q: read the order again before retrying", got, wantState)
	}
	return nil
}

// advanceExecutionOrder moves an execution-model order to the next state of
// its workflow: one Commit that updates the order and fires "advance" on its
// machine. Like the work order module it needs the header confirmed, and the
// step into the final state needs every block completed.
func (s *Service) advanceExecutionOrder(ctx context.Context, in AdvanceInput) (AdvanceResult, error) {
	userID, err := actingUser(in.UserID)
	if err != nil {
		return AdvanceResult{}, err
	}
	x, err := s.loadExecution(ctx, in.OrderID)
	if err != nil {
		return AdvanceResult{}, err
	}
	if in.WorkflowIndex != nil && *in.WorkflowIndex != x.idx {
		return AdvanceResult{}, rejection("stale_state", fmt.Sprintf("the order is at step %d, not %d: read it again", x.idx, *in.WorkflowIndex), x.state)
	}
	if x.idx == x.last {
		return AdvanceResult{}, rejection("final_state", fmt.Sprintf("the order is already in its final state %q", x.state), x.state)
	}
	if in.Input != InputComplete && in.Input != InputAdvance {
		return AdvanceResult{}, rejection("invalid_input", fmt.Sprintf("no transition for input %q from state %q: template workflows only accept %q", in.Input, x.state, InputComplete), x.state)
	}
	if len(in.HeaderValues) > 0 || len(in.BlockValues) > 0 {
		return AdvanceResult{}, &ValidationError{Fields: []string{"header_values", "block_values"},
			Reason: "this order records header answers with ConfirmExecutionHeader and block answers with ExecuteBlock; AdvanceOrder only moves the workflow"}
	}
	if !x.open {
		return AdvanceResult{}, closedRejection(x)
	}
	if err := s.requireOwner(ctx, x, userID); err != nil {
		return AdvanceResult{}, err
	}
	if !x.headerReady {
		return AdvanceResult{}, rejection("header_not_confirmed", "confirm the execution header (ConfirmExecutionHeader) before advancing", x.state)
	}
	next := x.tpl.States[x.idx+1]
	if x.idx+1 == x.last {
		var pending []string
		for i, b := range x.blocks {
			if b["state"] != blockCompleted {
				pending = append(pending, strings.TrimSpace(x.tpl.Blocks[i].Name))
			}
		}
		if len(pending) > 0 {
			return AdvanceResult{}, rejection("blocks_incomplete", "every block must be completed before the last step; pending: "+strings.Join(pending, ", "), x.state)
		}
	}
	version := intOf(x.doc["_version"])
	if version <= 0 {
		return AdvanceResult{}, fmt.Errorf("work order %s has no _version: cannot advance it safely", x.id)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	data := make(map[string]any, len(x.doc)+4)
	for k, v := range x.doc {
		if k != "_version" && k != "id" {
			data[k] = v
		}
	}
	data["state"], data["workflow_index"], data["updated_at"] = next, x.idx+1, now
	if x.idx+1 == x.last {
		data["completed_at"] = now
	}
	v := int(version)
	got, err := s.sub.CommitWalk(ctx, CommitRequest{
		Update:  CommitUpdate{Entity: EntityType, ID: intOf(x.id), Version: &v, Data: data},
		FSMWalk: &CommitFSMWalk{Machine: intOf(x.doc["machine_id"]), Input: InputAdvance, Payload: map[string]any{"work_order_id": numericOrString(x.id)}},
	})
	if err != nil {
		if r := staleOrConflict(err, x.state, "the order"); r != nil {
			return AdvanceResult{}, r
		}
		return AdvanceResult{}, classify(err, x.id)
	}
	if got != next {
		return AdvanceResult{}, fmt.Errorf("the workflow machine reached %q, expected %q: read the order again before retrying", got, next)
	}
	for k, val := range data {
		x.doc[k] = val
	}
	return AdvanceResult{Order: orderFromDoc(x.id, x.doc), Previous: x.state, Current: next}, nil
}
