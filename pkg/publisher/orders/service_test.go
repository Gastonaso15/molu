package orders

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeSubstrate is an in-memory xolu double that enforces the lifecycle FSM
// the way xolu does, answering with raw *XoluError values.
type fakeSubstrate struct {
	mu          sync.Mutex
	next        int
	machines    map[string]*Machine
	entities    map[string]map[string]any
	ensureCalls atomic.Int32
	ensureErr   error
	failWith    error // when set, every call returns it
}

func newFake() *fakeSubstrate {
	return &fakeSubstrate{machines: map[string]*Machine{}, entities: map[string]map[string]any{}}
}

func (f *fakeSubstrate) EnsureDefinition(_ context.Context, _ string, _ Definition) (string, error) {
	f.ensureCalls.Add(1)
	if f.ensureErr != nil {
		return "", f.ensureErr
	}
	return "42", f.failWith
}

func (f *fakeSubstrate) CreateMachine(_ context.Context, _ string, entityType string, entity map[string]any) (Machine, error) {
	if f.failWith != nil {
		return Machine{}, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := strconv.Itoa(1000 + f.next)
	m := &Machine{ID: id, State: "Open", Ref: entityType + ":" + id}
	f.machines[id] = m
	f.entities[m.Ref] = entity
	return *m, nil
}

func (f *fakeSubstrate) GetMachine(_ context.Context, id string) (Machine, error) {
	if f.failWith != nil {
		return Machine{}, f.failWith
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[id]
	if !ok {
		return Machine{}, &XoluError{Status: 404, Code: "XOLU-FSM001", Message: "machine not found"}
	}
	return *m, nil
}

func (f *fakeSubstrate) GetEntity(_ context.Context, entityType, id string) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entities[entityType+":"+id]
	if !ok {
		return nil, &XoluError{Status: 404, Code: "XOLU-ST001", Message: "entity not found"}
	}
	return e, nil
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
	case prev == "Open" && input == "start":
		m.State = "InProgress"
	case prev == "InProgress" && input == "complete":
		if r, _ := payload["resolution"].(string); r == "" {
			return WalkResult{}, &XoluError{Status: 422, Code: "XOLU-FSM004", Message: "transition guard rejected",
				Detail: "guard: payload.resolution IS NOT NULL AND payload.resolution != '' evaluated false", State: prev}
		}
		m.State, m.Terminal = "Completed", true
		return WalkResult{Previous: prev, Current: "Completed", Terminal: true, Outputs: []string{"order_completed"}}, nil
	case input == "cancel" && (prev == "Open" || prev == "InProgress"):
		m.State, m.Terminal = "Cancelled", true
		return WalkResult{Previous: prev, Current: "Cancelled", Terminal: true}, nil
	default:
		return WalkResult{}, &XoluError{Status: 409, Code: "XOLU-FSM003",
			Message:         fmt.Sprintf("no transition for input %q from state %q", input, prev),
			State:           prev,
			AvailableInputs: []string{"start", "cancel"}}
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
	if got.ID == "" || got.State != "Open" || got.Title != "Replace pump" || got.Description != "Pump A" || got.Terminal {
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
	if got != created {
		t.Fatalf("got %+v, want %+v", got, created)
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
	if res.Previous != "Open" || res.Current != "InProgress" || res.Order.State != "InProgress" || res.Order.Title != "t" {
		t.Fatalf("unexpected result: %+v", res)
	}

	done, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: "complete", Payload: map[string]any{"resolution": "fixed"}})
	if err != nil || !done.Order.Terminal || len(done.Outputs) != 1 {
		t.Fatalf("complete: %+v, %v", done, err)
	}
}

func TestOrderService_AdvanceOrder_InvalidTransitionIsVerbatim(t *testing.T) {
	svc := NewService(newFake())
	o, _ := svc.CreateOrder(context.Background(), CreateInput{Title: "t"})

	_, err := svc.AdvanceOrder(context.Background(), AdvanceInput{OrderID: o.ID, Input: "complete", Payload: map[string]any{"resolution": "x"}})
	var te *TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("want TransitionError, got %v", err)
	}
	if te.Code != "XOLU-INVALID_TRANSITION" {
		t.Errorf("code = %q", te.Code)
	}
	wantMsg := `no transition for input "complete" from state "Open"`
	if te.Message != wantMsg {
		t.Errorf("message = %q, want verbatim %q", te.Message, wantMsg)
	}
	if te.Details["xolu_code"] != "XOLU-FSM003" || te.Details["state"] != "Open" {
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
		te.Details["detail"] != "guard: payload.resolution IS NOT NULL AND payload.resolution != '' evaluated false" {
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
