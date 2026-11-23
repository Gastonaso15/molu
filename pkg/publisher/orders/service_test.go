package orders

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeSubstrate is an in-memory xolu double that enforces the legacy lifecycle
// FSM the way xolu does, answering with raw *XoluError values.
type fakeSubstrate struct {
	mu          sync.Mutex
	next        int
	machines    map[string]*Machine
	entities    map[string]map[string]map[string]any // type -> id -> doc
	ensureCalls atomic.Int32
	ensureErr   error
	failWith    error // when set, every call returns it
	patchErr    error
	history     map[string][]HistoryEntry // machine id -> FSM history
}

func newFake() *fakeSubstrate {
	return &fakeSubstrate{machines: map[string]*Machine{}, entities: map[string]map[string]map[string]any{}}
}

func (f *fakeSubstrate) EnsureDefinition(_ context.Context, _ string, _ Definition) (string, error) {
	f.ensureCalls.Add(1)
	if f.ensureErr != nil {
		return "", f.ensureErr
	}
	return "42", f.failWith
}

func (f *fakeSubstrate) CreateMachine(_ context.Context, _ string) (Machine, error) {
	if f.failWith != nil {
		return Machine{}, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := strconv.Itoa(1000 + f.next)
	m := &Machine{ID: id, State: StateDraft}
	f.machines[id] = m
	return *m, nil
}

func (f *fakeSubstrate) CreateEntity(_ context.Context, entityType string, data map[string]any) (string, error) {
	if f.failWith != nil {
		return "", f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := strconv.Itoa(f.next)
	f.put(entityType, id, data)
	return id, nil
}

// put stores a copy of data; callers hold f.mu.
func (f *fakeSubstrate) put(entityType, id string, data map[string]any) {
	if f.entities[entityType] == nil {
		f.entities[entityType] = map[string]map[string]any{}
	}
	doc := map[string]any{"id": id}
	for k, v := range data {
		doc[k] = v
	}
	f.entities[entityType][id] = doc
}

func (f *fakeSubstrate) GetEntity(_ context.Context, entityType, id string) (map[string]any, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entities[entityType][id]
	if !ok {
		return nil, &XoluError{Status: 404, Code: "XOLU-ST001", Message: "entity not found"}
	}
	cp := map[string]any{}
	for k, v := range e {
		cp[k] = v
	}
	return cp, nil
}

func (f *fakeSubstrate) PatchEntity(_ context.Context, entityType, id string, data map[string]any) error {
	if f.failWith != nil {
		return f.failWith
	}
	if f.patchErr != nil {
		return f.patchErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entities[entityType][id]
	if !ok {
		return &XoluError{Status: 404, Code: "XOLU-ST001", Message: "entity not found"}
	}
	// xolu's optimistic concurrency: a stale _version is a 409.
	if want, ok := data["_version"]; ok {
		if cur, has := e["_version"]; has && idString(cur) != idString(want) {
			return &XoluError{Status: 409, Code: "XOLU-ST009", Message: "version conflict"}
		}
	}
	for k, v := range data {
		e[k] = v
	}
	if cur, has := e["_version"]; has {
		e["_version"] = intOf(cur) + 1
	}
	return nil
}

func (f *fakeSubstrate) ListEntities(_ context.Context, entityType string) ([]map[string]any, error) {
	if f.failWith != nil {
		return nil, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, e := range f.entities[entityType] {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	return out, nil
}

func (f *fakeSubstrate) Walk(_ context.Context, id, input string, payload map[string]any) (WalkResult, error) {
	if f.failWith != nil {
		return WalkResult{}, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return WalkResult{}, &XoluError{Status: 404, Code: "XOLU-FSM001", Message: "machine not found"}
	}
	prev := m.State
	switch {
	case m.Terminal:
		return WalkResult{}, &XoluError{Status: 409, Code: "XOLU-FSM003",
			Message: fmt.Sprintf("machine is in terminal state %q", m.State), State: m.State}
	case prev == StateDraft && input == InputStart:
		m.State = StateInProgress
	case prev == StateInProgress && input == InputComplete:
		if r, _ := payload["technician_signature"].(string); r == "" {
			return WalkResult{}, &XoluError{Status: 422, Code: "XOLU-FSM004", Message: "transition guard rejected",
				Detail: "guard: payload.technician_signature != '' evaluated false", State: prev}
		}
		m.State, m.Terminal = StateCompleted, true
		return WalkResult{Previous: prev, Current: StateCompleted, Terminal: true}, nil
	default:
		return WalkResult{}, &XoluError{Status: 409, Code: "XOLU-FSM003",
			Message:         fmt.Sprintf("no transition for input %q from state %q", input, prev),
			State:           prev,
			AvailableInputs: []string{"start"}}
	}
	return WalkResult{Previous: prev, Current: m.State}, nil
}

func TestOrderService_CreateOrder(t *testing.T) {
	fs := newFake()
	svc := NewService(fs)

	got, err := svc.CreateOrder(context.Background(), CreateInput{Title: "Replace pump", Description: "Pump A"})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if got.ID == "" || got.State != StateDraft || got.Title != "Replace pump" || got.Description != "Pump A" || got.Terminal {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestOrderService_CreateOrder_RejectsEmptyTitle(t *testing.T) {
	svc := NewService(newFake())
	_, err := svc.CreateOrder(context.Background(), CreateInput{Title: "  "})
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0] != "title" {
		t.Fatalf("want ValidationError on title, got %v", err)
	}
}

func TestOrderService_EnsureDefinitionOnceUnderConcurrency(t *testing.T) {
	fs := newFake()
	svc := NewService(fs)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.CreateOrder(context.Background(), CreateInput{Title: "x"}); err != nil {
				t.Errorf("CreateOrder: %v", err)
			}
		}()
	}
	wg.Wait()
	if n := fs.ensureCalls.Load(); n != 1 {
		t.Fatalf("EnsureDefinition called %d times, want 1", n)
	}
}

func TestOrderService_EnsureDefinitionRetriesAfterFailure(t *testing.T) {
	fs := newFake()
	fs.ensureErr = &XoluError{Status: 500, Code: "XOLU-ST006", Message: "boom"}
	svc := NewService(fs)

	if _, err := svc.CreateOrder(context.Background(), CreateInput{Title: "x"}); err == nil {
		t.Fatal("expected failure while definition cannot be ensured")
	}
	fs.ensureErr = nil
	if _, err := svc.CreateOrder(context.Background(), CreateInput{Title: "x"}); err != nil {
		t.Fatalf("a later call must retry the definition: %v", err)
	}
}

func TestOrderService_GetOrder(t *testing.T) {
	svc := NewService(newFake())
	created, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "Inspect", Description: "d"})

	got, err := svc.GetOrder(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if !reflect.DeepEqual(got.WorkOrder, created) {
		t.Fatalf("got %+v, want %+v", got.WorkOrder, created)
	}
}

func TestOrderService_GetOrder_NotFound(t *testing.T) {
	svc := NewService(newFake())
	_, err := svc.GetOrder(context.Background(), "9999")
	var nf *NotFoundError
	if !errors.As(err, &nf) || nf.ID != "9999" {
		t.Fatalf("want NotFoundError(9999), got %v", err)
	}
}

func TestOrderService_GetOrder_EmptyID(t *testing.T) {
	_, err := NewService(newFake()).GetOrder(context.Background(), "")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields[0] != "order_id" {
		t.Fatalf("want ValidationError on order_id, got %v", err)
	}
}

func TestOrderService_AdvanceOrder_Success(t *testing.T) {
	svc := NewService(newFake())
	o, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "t"})

	res, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: "start"})
	if err != nil {
		t.Fatalf("AdvanceOrder: %v", err)
	}
	if res.Previous != StateDraft || res.Current != StateInProgress || res.Order.State != StateInProgress || res.Order.Title != "t" {
		t.Fatalf("unexpected result: %+v", res)
	}

	done, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: "complete", Payload: map[string]any{"technician_signature": "data:image/png;base64,AAAA"}})
	if err != nil || !done.Order.Terminal || done.Order.State != StateCompleted {
		t.Fatalf("complete: %+v, %v", done, err)
	}
}

func TestOrderService_AdvanceOrder_InvalidTransitionIsVerbatim(t *testing.T) {
	svc := NewService(newFake())
	o, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "t"})

	_, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: "complete", Payload: map[string]any{"technician_signature": "x"}})
	var te *TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("want TransitionError, got %v", err)
	}
	if te.Code != "XOLU-INVALID_TRANSITION" {
		t.Errorf("code = %q", te.Code)
	}
	wantMsg := `no transition for input "complete" from state "draft"`
	if te.Message != wantMsg {
		t.Errorf("message = %q, want verbatim %q", te.Message, wantMsg)
	}
	if te.Details["xolu_code"] != "XOLU-FSM003" || te.Details["state"] != StateDraft {
		t.Errorf("details = %v", te.Details)
	}
}

func TestOrderService_AdvanceOrder_GuardRejectionKeepsDiagnostic(t *testing.T) {
	svc := NewService(newFake())
	o, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "t"})
	_, _ = svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: "start"})

	_, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: "complete"})
	var te *TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("want TransitionError, got %v", err)
	}
	if te.Message != "transition guard rejected" {
		t.Errorf("message = %q", te.Message)
	}
	if te.Details["xolu_code"] != "XOLU-FSM004" ||
		te.Details["detail"] != "guard: payload.technician_signature != '' evaluated false" {
		t.Errorf("guard diagnostic lost: %v", te.Details)
	}
}

func TestOrderService_AdvanceOrder_NotFoundAndValidation(t *testing.T) {
	svc := NewService(newFake())

	_, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: "404", Input: "start"})
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %v", err)
	}

	_, err = svc.AdvanceOrder(context.Background(), AdvanceInput{})
	var ve *ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) != 2 {
		t.Fatalf("want ValidationError on order_id+input, got %v", err)
	}
}

func TestOrderService_SubstrateFailuresAreUnavailable(t *testing.T) {
	for name, cause := range map[string]error{
		"5xx":     &XoluError{Status: 500, Code: "XOLU-ST006", Message: "backend failure"},
		"network": errors.New("dial tcp: connection refused"),
	} {
		t.Run(name, func(t *testing.T) {
			fs := newFake()
			svc := NewService(fs)
			o, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "t"})
			fs.failWith = cause

			_, err := svc.GetOrder(context.Background(), o.ID)
			var ue *UnavailableError
			if !errors.As(err, &ue) {
				t.Fatalf("GetOrder: want UnavailableError, got %v", err)
			}
			_, err = svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: "start"})
			if !errors.As(err, &ue) {
				t.Fatalf("AdvanceOrder: want UnavailableError, got %v", err)
			}
			_, err = svc.CreateOrder(context.Background(), CreateInput{Title: "t2"})
			if !errors.As(err, &ue) {
				t.Fatalf("CreateOrder: want UnavailableError, got %v", err)
			}
		})
	}
}

func TestOrderService_CreateOrder_StoresDocumentLikeSeam(t *testing.T) {
	fs := newFake()
	svc := NewService(fs)
	got, err := svc.CreateOrder(context.Background(), CreateInput{Title: "t", AssignedTo: "ana@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	doc := fs.entities[EntityType][got.ID]
	if doc["state"] != StateDraft || doc["assigned_to"] != "ana@x.com" || doc["machine_id"] != int64(1001) {
		t.Fatalf("document = %v", doc)
	}
	if doc["created_at"] == nil || doc["updated_at"] == nil {
		t.Fatalf("timestamps missing: %v", doc)
	}
	if got.AssignedTo != "ana@x.com" {
		t.Fatalf("order = %+v", got)
	}
}

func TestOrderService_AdvanceOrder_DenormalizesStateAndSignature(t *testing.T) {
	fs := newFake()
	svc := NewService(fs)
	o, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "t"})

	_, _ = svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: InputStart})
	if st := fs.entities[EntityType][o.ID]["state"]; st != StateInProgress {
		t.Fatalf("state on document = %v", st)
	}
	_, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "sig"}})
	if err != nil {
		t.Fatal(err)
	}
	doc := fs.entities[EntityType][o.ID]
	if doc["state"] != StateCompleted || doc["technician_signature"] != "sig" {
		t.Fatalf("document = %v", doc)
	}
	got, _ := svc.GetOrder(context.Background(), o.ID)
	if !got.Terminal || got.State != StateCompleted {
		t.Fatalf("GetOrder = %+v", got)
	}
}

func TestOrderService_AdvanceOrder_RejectedWalkLeavesDocumentUntouched(t *testing.T) {
	fs := newFake()
	svc := NewService(fs)
	o, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "t"})
	_, _ = svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: InputStart})
	_, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: InputComplete})
	if err == nil {
		t.Fatal("expected guard rejection")
	}
	if st := fs.entities[EntityType][o.ID]["state"]; st != StateInProgress {
		t.Fatalf("state must not change on a rejected walk, got %v", st)
	}
}

func TestOrderService_AdvanceOrder_PatchFailureIsReported(t *testing.T) {
	fs := newFake()
	svc := NewService(fs)
	o, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "t"})
	fs.patchErr = &XoluError{Status: 500, Code: "XOLU-ST006", Message: "boom"}
	_, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: InputStart})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("want UnavailableError so the drift is not hidden, got %v", err)
	}
}

func TestOrderService_AdvanceOrder_OrderWithoutMachineIsRejected(t *testing.T) {
	fs := newFake()
	fs.put(EntityType, "77", map[string]any{"title": "orphan", "state": "draft", "machine_id": int64(0)})
	_, err := NewService(fs).AdvanceOrder(context.Background(), AdvanceInput{OrderID: "77", Input: InputStart})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields[0] != "order_id" || ve.Reason == "" {
		t.Fatalf("want ValidationError with a reason, got %v", err)
	}
}

func seedTemplates(fs *fakeSubstrate) {
	fs.put(TemplateEntityType, "1", map[string]any{"name": "Pump check", "version": 1, "latest": false,
		"workflow_states": []any{map[string]any{"name": "Open"}, map[string]any{"name": "Done"}}})
	fs.put(TemplateEntityType, "2", map[string]any{"name": "Pump check", "version": 2, "latest": true, "description": "v2", "asset_type_id": 9,
		"workflow_states": []any{map[string]any{"name": "Open"}, map[string]any{"name": "Review"}, map[string]any{"name": "Done"}}})
}

func TestOrderService_ListTemplates_LatestOnlyByDefault(t *testing.T) {
	fs := newFake()
	seedTemplates(fs)
	got, err := NewService(fs).ListTemplates(context.Background(), ListTemplatesInput{})
	if err != nil || len(got.Templates) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	tpl := got.Templates[0]
	if tpl.ID != "2" || tpl.Version != 2 || !tpl.Latest || tpl.AssetTypeID != "9" || len(tpl.WorkflowStates) != 3 || tpl.WorkflowStates[1] != "Review" {
		t.Fatalf("template = %+v", tpl)
	}
}

func TestOrderService_ListTemplates_IncludeOldVersions(t *testing.T) {
	fs := newFake()
	seedTemplates(fs)
	got, _ := NewService(fs).ListTemplates(context.Background(), ListTemplatesInput{IncludeOldVersions: true})
	if len(got.Templates) != 2 {
		t.Fatalf("got %d templates, want 2", len(got.Templates))
	}
}

func TestOrderService_ListTemplates_NoneYetIsEmptyNotError(t *testing.T) {
	got, err := NewService(newFake()).ListTemplates(context.Background(), ListTemplatesInput{})
	if err != nil || got.Templates == nil || len(got.Templates) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOrderService_ListTemplates_MissingEntityIsEmpty(t *testing.T) {
	fs := newFake()
	fs.failWith = &XoluError{Status: 400, Code: "XOLU-QL004", Message: "entity 'work_order_templates' does not exist"}
	got, err := NewService(fs).ListTemplates(context.Background(), ListTemplatesInput{})
	if err != nil || len(got.Templates) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOrderService_ListTemplates_SubstrateDown(t *testing.T) {
	fs := newFake()
	fs.failWith = errors.New("connection refused")
	_, err := NewService(fs).ListTemplates(context.Background(), ListTemplatesInput{})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("want UnavailableError, got %v", err)
	}
}
