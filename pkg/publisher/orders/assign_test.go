package orders

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func seedAssignable(fs *fakeSubstrate) {
	fs.put("users", "9", map[string]any{"name": "Ana", "email": "ana@example.com", "password_hash": "secret", "active": true})
	fs.put("users", "10", map[string]any{"name": "Carlos", "active": false})
	fs.put("users", "11", map[string]any{"name": "   ", "active": true})
	fs.put("crews", "7", map[string]any{"name": "Cuadrilla A", "responsible_user_id": 9, "members": []any{9}})
	fs.put("crews", "8", map[string]any{"name": "Cuadrilla B", "responsible_user_id": 9})
	fs.put("crews", "12", map[string]any{"name": ""})
	fs.put("outsourced", "3", map[string]any{"name": "Tercero SA", "responsible_user_id": 9})

	// Unclaimed board order (template)
	fs.put(EntityType, "40", map[string]any{
		"title": "Orden tablero", "state": "Pendiente", "workflow_mode": "template", "workflow_index": 0,
		"template_id": 1, "assignment_type": AssignmentBoard, "responsible_type": "crew", "responsible_id": 0,
		"assigned_to": "", "_version": 1, "created_at": "2026-11-23T10:00:00Z",
	})
	// Claimed board order (template)
	fs.put(EntityType, "41", map[string]any{
		"title": "Orden reclamada", "state": "En curso", "workflow_mode": "template", "workflow_index": 1,
		"template_id": 1, "assignment_type": AssignmentBoard, "responsible_type": "crew", "responsible_id": 7,
		"claimed_by_user_id": 9, "assigned_to": "Cuadrilla A", "_version": 2, "created_at": "2026-11-23T10:00:00Z",
	})
	// Direct assigned order (template)
	fs.put(EntityType, "42", map[string]any{
		"title": "Orden directa", "state": "Pendiente", "workflow_mode": "template", "workflow_index": 0,
		"template_id": 1, "assignment_type": AssignmentDirect, "responsible_type": "crew", "responsible_id": 7,
		"assigned_to": "Cuadrilla A", "_version": 1, "created_at": "2026-11-23T10:00:00Z",
	})
	// Completed template order
	fs.put(EntityType, "43", map[string]any{
		"title": "Orden cerrada", "state": "Cerrada", "workflow_mode": "template", "workflow_index": 2,
		"template_id": 1, "assignment_type": AssignmentDirect, "responsible_type": "crew", "responsible_id": 7,
		"assigned_to": "Cuadrilla A", "completed_at": "2026-11-23T12:00:00Z", "_version": 3,
	})
	// Legacy open order
	fs.put(EntityType, "44", map[string]any{
		"title": "Legacy abierta", "state": StateDraft, "machine_id": 1001, "_version": 1,
	})
	// Legacy completed order
	fs.put(EntityType, "45", map[string]any{
		"title": "Legacy cerrada", "state": StateCompleted, "machine_id": 1002, "_version": 2,
	})
}

func TestAssignOrder_DirectToCrewDefaultAssignmentType(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)
	svc := NewService(fs)

	yes := true
	before, err := svc.ListOrders(ctx, ListOrdersInput{Backlog: &yes})
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.AssignOrder(ctx, AssignInput{
		OrderID:         "40",
		ResponsibleType: "crew",
		ResponsibleID:   "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyAssigned || res.Reassigned {
		t.Fatalf("flags = already:%v reassigned:%v, want false/false", res.AlreadyAssigned, res.Reassigned)
	}
	if res.Responsible == nil || res.Responsible.ID != "7" || res.Responsible.Name != "Cuadrilla A" {
		t.Fatalf("responsible = %+v", res.Responsible)
	}
	if res.Order.AssignmentType != AssignmentDirect || res.Order.ResponsibleType != "crew" ||
		res.Order.ResponsibleID != "7" || res.Order.AssignedTo != "Cuadrilla A" {
		t.Fatalf("order = %+v", res.Order)
	}

	doc := fs.entities[EntityType]["40"]
	if doc["assignment_type"] != AssignmentDirect || doc["responsible_type"] != "crew" ||
		intOf(doc["responsible_id"]) != 7 || doc["assigned_to"] != "Cuadrilla A" ||
		intOf(doc["claimed_by_user_id"]) != 0 || doc["updated_at"] == nil {
		t.Fatalf("document = %v", doc)
	}

	after, err := svc.ListOrders(ctx, ListOrdersInput{Backlog: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if after.Total != before.Total-1 {
		t.Fatalf("backlog %d -> %d, want %d", before.Total, after.Total, before.Total-1)
	}
}

func TestAssignOrder_DirectToUserAndInactiveChecks(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)
	svc := NewService(fs)

	res, err := svc.AssignOrder(ctx, AssignInput{
		OrderID:         "40",
		AssignmentType:  AssignmentDirect,
		ResponsibleType: "user",
		ResponsibleID:   "9",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Responsible == nil || res.Responsible.ID != "9" || res.Responsible.Name != "Ana" || res.Order.AssignedTo != "Ana" {
		t.Fatalf("result = %+v", res)
	}

	// Inactive user is rejected.
	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "user", ResponsibleID: "10"})
	wantInvalid(t, err, "responsible_id")

	// User without a name is rejected.
	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "user", ResponsibleID: "11"})
	wantInvalid(t, err, "responsible_id")

	// Unknown user is rejected.
	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "user", ResponsibleID: "999"})
	wantInvalid(t, err, "responsible_id")
}

func TestAssignOrder_DirectToThirdPartyAndReassignClearsClaim(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)
	svc := NewService(fs)

	res, err := svc.AssignOrder(ctx, AssignInput{
		OrderID:         "41",
		AssignmentType:  AssignmentDirect,
		ResponsibleType: "third_party",
		ResponsibleID:   "3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reassigned || res.AlreadyAssigned {
		t.Fatalf("flags = reassigned:%v already:%v, want true/false", res.Reassigned, res.AlreadyAssigned)
	}
	if res.Responsible == nil || res.Responsible.ID != "3" || res.Responsible.Name != "Tercero SA" {
		t.Fatalf("responsible = %+v", res.Responsible)
	}
	doc := fs.entities[EntityType]["41"]
	if intOf(doc["claimed_by_user_id"]) != 0 || doc["assignment_type"] != AssignmentDirect ||
		doc["responsible_type"] != "third_party" || intOf(doc["responsible_id"]) != 3 || doc["assigned_to"] != "Tercero SA" {
		t.Fatalf("document = %v", doc)
	}
}

func TestAssignOrder_BoardPostingReturnsToBacklog(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)
	svc := NewService(fs)

	res, err := svc.AssignOrder(ctx, AssignInput{
		OrderID:         "41",
		AssignmentType:  AssignmentBoard,
		ResponsibleType: "third_party",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reassigned || res.AlreadyAssigned || res.Responsible != nil {
		t.Fatalf("result = %+v", res)
	}
	if res.Order.AssignmentType != AssignmentBoard || res.Order.ResponsibleType != "third_party" ||
		res.Order.ResponsibleID != "" || res.Order.AssignedTo != "" {
		t.Fatalf("order = %+v", res.Order)
	}
	doc := fs.entities[EntityType]["41"]
	if intOf(doc["responsible_id"]) != 0 || intOf(doc["claimed_by_user_id"]) != 0 || doc["assigned_to"] != "" {
		t.Fatalf("document = %v", doc)
	}

	yes := true
	list, err := svc.ListOrders(ctx, ListOrdersInput{Backlog: &yes})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range list.Orders {
		if o.ID == "41" {
			found = true
		}
	}
	if !found {
		t.Fatalf("order 41 should be back on the backlog: %+v", list.Orders)
	}
}

func TestAssignOrder_IdempotentDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)
	svc := NewService(fs)

	// Direct assignment identical to current state.
	vBefore := fs.entities[EntityType]["42"]["_version"]
	res, err := svc.AssignOrder(ctx, AssignInput{
		OrderID:         "42",
		AssignmentType:  AssignmentDirect,
		ResponsibleType: "crew",
		ResponsibleID:   "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyAssigned || res.Reassigned || res.Responsible == nil || res.Responsible.ID != "7" || res.Responsible.Name != "Cuadrilla A" {
		t.Fatalf("result = %+v", res)
	}
	if fs.entities[EntityType]["42"]["_version"] != vBefore {
		t.Error("idempotent direct assign must not write")
	}

	// Board posting identical to current state.
	vBoard := fs.entities[EntityType]["40"]["_version"]
	resBoard, err := svc.AssignOrder(ctx, AssignInput{
		OrderID:         "40",
		AssignmentType:  AssignmentBoard,
		ResponsibleType: "crew",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resBoard.AlreadyAssigned || resBoard.Reassigned || resBoard.Responsible != nil {
		t.Fatalf("board result = %+v", resBoard)
	}
	if fs.entities[EntityType]["40"]["_version"] != vBoard {
		t.Error("idempotent board posting must not write")
	}
}

func TestAssignOrder_LegacyAndTerminalOrders(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)
	svc := NewService(fs)

	// Open legacy order can be assigned.
	res, err := svc.AssignOrder(ctx, AssignInput{OrderID: "44", ResponsibleType: "crew", ResponsibleID: "8"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reassigned || res.Order.AssignedTo != "Cuadrilla B" || res.Order.ResponsibleID != "8" {
		t.Fatalf("legacy assign = %+v", res)
	}

	// Completed legacy order is rejected as final_state.
	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "45", ResponsibleType: "crew", ResponsibleID: "7"})
	te := wantRule(t, err, "final_state")
	if te.Details["state"] != StateCompleted {
		t.Errorf("details = %v", te.Details)
	}

	// Completed template order is rejected as final_state.
	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "43", ResponsibleType: "crew", ResponsibleID: "8"})
	te = wantRule(t, err, "final_state")
	if te.Details["state"] != "Cerrada" {
		t.Errorf("details = %v", te.Details)
	}
}

func TestAssignOrder_NotFoundAndValidation(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)
	svc := NewService(fs)

	_, err := svc.AssignOrder(ctx, AssignInput{OrderID: "999", ResponsibleType: "crew", ResponsibleID: "7"})
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %T %v, want NotFoundError", err, err)
	}

	_, err = svc.AssignOrder(ctx, AssignInput{ResponsibleType: "crew", ResponsibleID: "7"})
	wantInvalid(t, err, "order_id")

	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", AssignmentType: "other", ResponsibleType: "crew", ResponsibleID: "7"})
	wantInvalid(t, err, "assignment_type")

	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "robot", ResponsibleID: "7"})
	wantInvalid(t, err, "responsible_type")

	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: ""})
	wantInvalid(t, err, "responsible_type")

	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", AssignmentType: AssignmentDirect, ResponsibleType: "crew"})
	wantInvalid(t, err, "responsible_id")

	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", AssignmentType: AssignmentDirect, ResponsibleType: "crew", ResponsibleID: "0"})
	wantInvalid(t, err, "responsible_id")

	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", AssignmentType: AssignmentBoard, ResponsibleType: "crew", ResponsibleID: "7"})
	wantInvalid(t, err, "responsible_id")

	// Missing crew / nameless crew / missing outsourced
	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "crew", ResponsibleID: "999"})
	wantInvalid(t, err, "responsible_id")

	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "crew", ResponsibleID: "12"})
	wantInvalid(t, err, "responsible_id")

	_, err = svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "third_party", ResponsibleID: "999"})
	wantInvalid(t, err, "responsible_id")
}

func TestAssignOrder_VersionConflictMissingVersionAndSubstrateDown(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)

	// 409 version conflict -> stale_state
	fs.patchErr = &XoluError{Status: 409, Code: "XOLU-ST005", Message: "version conflict"}
	_, err := NewService(fs).AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "crew", ResponsibleID: "7"})
	wantRule(t, err, "stale_state")
	fs.patchErr = nil

	// Order without _version -> error (refuses unsafe write)
	fs.put(EntityType, "46", map[string]any{"title": "Sin version", "state": "Pendiente"})
	if _, err := NewService(fs).AssignOrder(ctx, AssignInput{OrderID: "46", ResponsibleType: "crew", ResponsibleID: "7"}); err == nil {
		t.Fatal("order without _version should fail")
	}

	// Substrate down -> UnavailableError
	fs.failWith = &XoluError{Status: 503, Message: "down"}
	_, err = NewService(fs).AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "crew", ResponsibleID: "7"})
	var ue *UnavailableError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %T %v, want UnavailableError", err, err)
	}
}

func TestAssignOrder_ConcurrentSafe(t *testing.T) {
	ctx := context.Background()
	fs := newFake()
	seedAssignable(fs)
	svc := NewService(fs)

	var wg sync.WaitGroup
	targets := []ID{"7", "8"}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		target := targets[i%len(targets)]
		go func() {
			defer wg.Done()
			_, err := svc.AssignOrder(ctx, AssignInput{OrderID: "40", ResponsibleType: "crew", ResponsibleID: target})
			if err != nil {
				var te *TransitionError
				if !errors.As(err, &te) || te.Details["rule"] != "stale_state" {
					t.Errorf("unexpected error: %v", err)
				}
			}
		}()
	}
	wg.Wait()
}

func TestOrderServer_Assign(t *testing.T) {
	fs := newFake()
	seedAssignable(fs)
	srv := newTestServer(fs)
	defer srv.Close()

	resp, out := post(t, srv.URL+"/api/v1/orders/assign", testToken, `{"order_id":"40","responsible_type":"crew","responsible_id":7}`)
	if resp.StatusCode != 200 || out["reassigned"] != nil {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}
	orderMap := out["order"].(map[string]any)
	if orderMap["assigned_to"] != "Cuadrilla A" || orderMap["responsible_id"] != "7" {
		t.Fatalf("order = %v", orderMap)
	}

	// Reassign to board_posting
	resp, out = post(t, srv.URL+"/api/v1/orders/assign", testToken, `{"order_id":"40","assignment_type":"board_posting","responsible_type":"user"}`)
	if resp.StatusCode != 200 || out["reassigned"] != true {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}

	// Completed order -> 409 XOLU-INVALID_TRANSITION (final_state)
	resp, out = post(t, srv.URL+"/api/v1/orders/assign", testToken, `{"order_id":"43","responsible_type":"crew","responsible_id":8}`)
	if resp.StatusCode != 409 || out["code"] != "XOLU-INVALID_TRANSITION" || out["details"].(map[string]any)["rule"] != "final_state" {
		t.Fatalf("%d %v", resp.StatusCode, out)
	}

	// Not found -> 404
	resp, _ = post(t, srv.URL+"/api/v1/orders/assign", testToken, `{"order_id":"999","responsible_type":"crew","responsible_id":7}`)
	if resp.StatusCode != 404 {
		t.Errorf("not found: %d", resp.StatusCode)
	}

	// Bad request -> 400
	resp, _ = post(t, srv.URL+"/api/v1/orders/assign", testToken, `{"order_id":"40"}`)
	if resp.StatusCode != 400 {
		t.Errorf("missing responsible_type: %d", resp.StatusCode)
	}

	// Missing token -> 401
	resp, _ = post(t, srv.URL+"/api/v1/orders/assign", "", `{}`)
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
}
