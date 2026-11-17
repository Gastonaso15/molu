package orders

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Runs only when MOLU_REAL_XOLU_URL is set; exercises the service against a live xolu.
func TestRealXolu_Lifecycle(t *testing.T) {
	url := os.Getenv("MOLU_REAL_XOLU_URL")
	if url == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}
	ctx := context.Background()
	svc := NewService(NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0))

	o, err := svc.CreateOrder(ctx, CreateInput{Title: "Real xolu", Description: "d", AssignedTo: "ana"})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	t.Logf("created: %+v", o)
	got, err := svc.GetOrder(ctx, o.ID)
	t.Logf("get: %+v err=%v", got, err)
	if err != nil || got.Title != "Real xolu" || got.State != StateDraft || got.AssignedTo != "ana" {
		t.Fatalf("GetOrder mismatch")
	}

	// complete from draft: no such transition.
	_, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "x"}})
	var te *TransitionError
	if !errors.As(err, &te) {
		t.Fatalf("want TransitionError, got %T %v", err, err)
	}
	t.Logf("illegal advance: message=%q details=%v", te.Message, te.Details)

	r, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputStart})
	if err != nil || r.Current != StateInProgress {
		t.Fatalf("start: %+v err=%v", r, err)
	}

	// complete without a signature: the guard rejects it, verbatim.
	_, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete})
	if !errors.As(err, &te) {
		t.Fatalf("want guard TransitionError, got %T %v", err, err)
	}
	t.Logf("guard rejection: message=%q details=%v", te.Message, te.Details)

	r, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: InputComplete, Payload: map[string]any{"technician_signature": "data:image/png;base64,AAAA"}})
	if err != nil || !r.Order.Terminal || r.Order.State != StateCompleted {
		t.Fatalf("complete: %+v err=%v", r, err)
	}
	if again, err := svc.GetOrder(ctx, o.ID); err != nil || again.State != StateCompleted {
		t.Fatalf("state not denormalized: %+v err=%v", again, err)
	}

	_, err = svc.GetOrder(ctx, "99999999")
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("unknown order: want NotFoundError, got %T %v", err, err)
	}

	// Templates: store one with seam-CURE's document shape and read it back.
	sub := NewHTTPSubstrate(url, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 0)
	if _, err := sub.CreateEntity(ctx, TemplateEntityType, map[string]any{
		"name": "Real xolu template", "version": 1, "latest": true,
		"workflow_states": []map[string]any{{"name": "Open"}, {"name": "Review"}, {"name": "Done"}},
	}); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	tpls, err := svc.ListTemplates(ctx, ListTemplatesInput{})
	t.Logf("templates: %+v err=%v", tpls, err)
	if err != nil || len(tpls.Templates) == 0 {
		t.Fatalf("ListTemplates: %+v %v", tpls, err)
	}
	if first := tpls.Templates[0]; first.Name != "Real xolu template" || len(first.WorkflowStates) != 3 || !first.Latest {
		t.Fatalf("template parsed wrong: %+v", first)
	}
}
