package orders

import (
	"context"
	"errors"
	"testing"
)

func seedBoard(fs *fakeSubstrate) {
	fs.put("users", "9", map[string]any{"name": "Ana"})
	fs.put("crews", "7", map[string]any{"name": "Cuadrilla A", "responsible_user_id": 9, "members": []any{9}})
	fs.put("crews", "8", map[string]any{"name": "Cuadrilla B", "responsible_user_id": 9})
	fs.put("crews", "70", map[string]any{"name": "Ajena", "responsible_user_id": 5})
	fs.put("outsourced", "3", map[string]any{"name": "Tercero SA", "responsible_user_id": 9})
	board := func(id, rtype string) {
		fs.put(EntityType, id, map[string]any{
			"title": "Orden " + id, "state": "Pendiente", "workflow_mode": "template", "workflow_index": 0,
			"template_id": 1, "assignment_type": AssignmentBoard, "responsible_type": rtype, "responsible_id": 0,
			"assigned_to": "", "_version": 1, "created_at": "2026-11-10T10:00:00Z",
		})
	}
	board("30", "user")
	board("31", "crew")
	board("32", "third_party")
	fs.put(EntityType, "33", map[string]any{"title": "Directa", "state": "Pendiente", "workflow_mode": "template",
		"assignment_type": AssignmentDirect, "responsible_type": "crew", "responsible_id": 7, "_version": 1})
}

func TestClaimOrder_UserOrder(t *testing.T) {
	fs := newFake()
	seedBoard(fs)
	svc := NewService(fs)
	res, err := svc.ClaimOrder(context.Background(), ClaimInput{OrderID: "30", UserID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyClaimed || res.ClaimedByUserID != "9" || res.Responsible.ID != "9" || res.Responsible.Name != "Ana" ||
		res.Order.ResponsibleID != "9" || res.Order.AssignedTo != "Ana" {
		t.Fatalf("result = %+v", res)
	}
	doc := fs.entities[EntityType]["30"]
	if intOf(doc["claimed_by_user_id"]) != 9 || intOf(doc["responsible_id"]) != 9 || doc["assigned_to"] != "Ana" || doc["updated_at"] == nil {
		t.Fatalf("document = %v", doc)
	}
	// A user order can only be claimed by the user it is for.
	_, err = svc.ClaimOrder(context.Background(), ClaimInput{OrderID: "30", UserID: "9", ResponsibleID: "10"})
	wantRule(t, err, "already_claimed")
}

func TestClaimOrder_UserWithoutNameFallsBackToId(t *testing.T) {
	fs := newFake()
	seedBoard(fs)
	delete(fs.entities, "users")
	res, err := NewService(fs).ClaimOrder(context.Background(), ClaimInput{OrderID: "30", UserID: "9"})
	if err != nil || res.Responsible.Name != "#9" || res.Order.AssignedTo != "#9" {
		t.Fatalf("result = %+v err=%v", res, err)
	}
}

func TestClaimOrder_CrewEligibility(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedBoard(fs)
	svc := NewService(fs)

	// User 9 answers for two crews: the choice is theirs.
	_, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "9"})
	ve := wantInvalid(t, err, "responsible_id")
	if ve.Reason == "" {
		t.Error("the error should list the eligible crews")
	}
	// A crew of someone else is not eligible.
	_, err = svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "9", ResponsibleID: "70"})
	wantRule(t, err, "ineligible")
	// A user who answers for no crew cannot claim.
	_, err = svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "44"})
	wantRule(t, err, "ineligible")
	if intOf(fs.entities[EntityType]["31"]["responsible_id"]) != 0 {
		t.Fatal("a rejected claim must not change the order")
	}
	res, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "9", ResponsibleID: "8"})
	if err != nil || res.Responsible.ID != "8" || res.Responsible.Name != "Cuadrilla B" || res.Order.AssignedTo != "Cuadrilla B" || res.Order.ResponsibleType != "crew" {
		t.Fatalf("result = %+v err=%v", res, err)
	}
}

func TestClaimOrder_SingleEligibleIsChosen(t *testing.T) {
	fs := newFake()
	seedBoard(fs)
	delete(fs.entities["crews"], "8")
	res, err := NewService(fs).ClaimOrder(context.Background(), ClaimInput{OrderID: "31", UserID: "9"})
	if err != nil || res.Responsible.ID != "7" || res.Order.AssignedTo != "Cuadrilla A" {
		t.Fatalf("result = %+v err=%v", res, err)
	}
}

func TestClaimOrder_ThirdParty(t *testing.T) {
	fs := newFake()
	seedBoard(fs)
	svc := NewService(fs)
	res, err := svc.ClaimOrder(context.Background(), ClaimInput{OrderID: "32", UserID: "9"})
	if err != nil || res.Responsible.ID != "3" || res.Responsible.Name != "Tercero SA" {
		t.Fatalf("result = %+v err=%v", res, err)
	}
	_, err = NewService(fs).ClaimOrder(context.Background(), ClaimInput{OrderID: "32", UserID: "5"})
	wantRule(t, err, "already_claimed")
}

func TestClaimOrder_IdempotentForSameClaimantAndConflictForOthers(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedBoard(fs)
	svc := NewService(fs)
	if _, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "9", ResponsibleID: "7"}); err != nil {
		t.Fatal(err)
	}
	version := fs.entities[EntityType]["31"]["_version"]
	again, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "9", ResponsibleID: "7"})
	if err != nil || !again.AlreadyClaimed || again.Responsible.ID != "7" {
		t.Fatalf("again = %+v err=%v", again, err)
	}
	if fs.entities[EntityType]["31"]["_version"] != version {
		t.Error("an idempotent claim must not write")
	}
	// Omitting the responsible on a repeat means "the one I already claimed with".
	if r, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "9"}); err != nil || !r.AlreadyClaimed {
		t.Fatalf("repeat without responsible = %+v err=%v", r, err)
	}
	_, err = svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "9", ResponsibleID: "8"})
	wantRule(t, err, "already_claimed") // same user, other crew
	_, err = svc.ClaimOrder(ctx, ClaimInput{OrderID: "31", UserID: "5"})
	te := wantRule(t, err, "already_claimed") // someone else
	if te.Details["state"] != "Pendiente" {
		t.Errorf("details = %v", te.Details)
	}
}

func TestClaimOrder_NotOnTheBoardNotFoundAndValidation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedBoard(fs)
	svc := NewService(fs)
	_, err := svc.ClaimOrder(ctx, ClaimInput{OrderID: "33", UserID: "9"})
	wantRule(t, err, "not_claimable")
	fs.put(EntityType, "34", map[string]any{"title": "Legacy", "state": "draft", "machine_id": 1})
	_, err = svc.ClaimOrder(ctx, ClaimInput{OrderID: "34", UserID: "9"})
	wantRule(t, err, "not_claimable")
	_, err = svc.ClaimOrder(ctx, ClaimInput{OrderID: "999", UserID: "9"})
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %T %v", err, err)
	}
	_, err = svc.ClaimOrder(ctx, ClaimInput{UserID: "9"})
	wantInvalid(t, err, "order_id")
	for _, u := range []ID{"", "abc", "0"} {
		_, err = svc.ClaimOrder(ctx, ClaimInput{OrderID: "30", UserID: u})
		wantInvalid(t, err, "user_id")
	}
	_, err = svc.ClaimOrder(ctx, ClaimInput{OrderID: "30", UserID: "9", ResponsibleID: "x"})
	wantInvalid(t, err, "responsible_id")
}

func TestClaimOrder_ConcurrentClaimLosesCleanly(t *testing.T) {
	fs := newFake()
	seedBoard(fs)
	fs.patchErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "version conflict"}
	_, err := NewService(fs).ClaimOrder(context.Background(), ClaimInput{OrderID: "30", UserID: "9"})
	wantRule(t, err, "already_claimed")
}

func TestClaimOrder_SubstrateDown(t *testing.T) {
	fs := newFake()
	seedBoard(fs)
	fs.failWith = &XoluError{Status: 503, Message: "down"}
	_, err := NewService(fs).ClaimOrder(context.Background(), ClaimInput{OrderID: "30", UserID: "9"})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %T %v", err, err)
	}
}

func TestClaimOrder_LeavesTheBacklog(t *testing.T) {
	fs := newFake()
	seedBoard(fs)
	svc := NewService(fs)
	yes := true
	before, _ := svc.ListOrders(context.Background(), ListOrdersInput{Backlog: &yes})
	if _, err := svc.ClaimOrder(context.Background(), ClaimInput{OrderID: "30", UserID: "9"}); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.ListOrders(context.Background(), ListOrdersInput{Backlog: &yes})
	if after.Total != before.Total-1 {
		t.Fatalf("backlog %d -> %d", before.Total, after.Total)
	}
	d, _ := svc.GetOrder(context.Background(), "30")
	if d.ClaimedByUserID != "9" || d.AssignedTo != "Ana" {
		t.Fatalf("detail = %+v", d)
	}
}

func TestOrderServer_Claim(t *testing.T) {
	fs := newFake()
	seedBoard(fs)
	srv := newTestServer(fs)
	defer srv.Close()
	resp, out := post(t, srv.URL+"/api/v1/orders/claim", testToken, `{"order_id":"31","user_id":9,"responsible_id":7}`)
	if resp.StatusCode != 200 || out["claimed_by_user_id"] != "9" {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	resp, out = post(t, srv.URL+"/api/v1/orders/claim", testToken, `{"order_id":"31","user_id":5}`)
	if resp.StatusCode != 409 || out["code"] != "XOLU-INVALID_TRANSITION" || out["details"].(map[string]any)["rule"] != "already_claimed" {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/claim", testToken, `{"order_id":"31"}`)
	if resp.StatusCode != 400 {
		t.Errorf("no user: %d", resp.StatusCode)
	}
	resp, _ = post(t, srv.URL+"/api/v1/orders/claim", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}
