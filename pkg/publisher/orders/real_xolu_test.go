package orders

import (
	"context"
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
	svc := NewService(NewHTTPSubstrate(url, "", 0))

	o, err := svc.CreateOrder(ctx, CreateInput{Title: "Real xolu", Description: "d"})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	t.Logf("created: %+v", o)
	got, err := svc.GetOrder(ctx, o.ID)
	t.Logf("get: %+v err=%v", got, err)
	if err != nil || got.Title != "Real xolu" || got.State != "Open" {
		t.Fatalf("GetOrder mismatch")
	}
	_, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: "complete", Payload: map[string]any{"resolution": "x"}})
	t.Logf("illegal advance err: %#v", err)
	if te, ok := err.(*TransitionError); !ok {
		t.Fatalf("want TransitionError, got %T %v", err, err)
	} else {
		t.Logf("message=%q details=%v", te.Message, te.Details)
	}
	r, err := svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: "start"})
	t.Logf("start: %+v err=%v", r, err)
	_, err = svc.AdvanceOrder(ctx, AdvanceInput{OrderID: o.ID, Input: "complete"})
	t.Logf("guard err: %#v", err)
	_, err = svc.GetOrder(ctx, "99999999")
	t.Logf("unknown: %#v", err)
}
