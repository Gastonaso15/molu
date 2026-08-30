package semantic

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSemanticStore_Describe(t *testing.T) {
	store := NewSemanticStore("tenant-test")

	mockMap := &SemanticMap{
		Entities: map[string]*EntityDef{
			"Order": {
				Name:   "Order",
				Schema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
				Fields: []FieldDef{
					{Name: "id", Type: "string", Required: true},
					{Name: "amount", Type: "number"},
				},
				Refs: []RefFieldDef{
					{Field: "customer_id", TargetEntity: "Customer"},
				},
			},
		},
		Machines: map[string]*MachineDef{
			"OrderFSM": {
				ID:           "OrderFSM",
				EntityType:   "Order",
				InitialState: "draft",
				States: map[string]StateDef{
					"draft":     {Name: "draft"},
					"completed": {Name: "completed", Terminal: true},
				},
				Transitions: []TransitionDef{
					{From: "draft", Input: "submit", To: "completed", Guard: "amount > 0"},
				},
			},
		},
		Sequences: map[string]*GenDef{
			"order_num_seq": {Name: "order_num_seq", Kind: "sequence"},
		},
		Events: map[string]*EventDef{
			"order_created": {ID: "order_created", EventType: "entity.created"},
		},
		Tenant: "tenant-test",
		ReadAt: time.Now(),
	}

	store.Update(mockMap)
	cur := store.Get()

	// 1. Describe all
	res, err := cur.Describe("all", "")
	if err != nil {
		t.Fatalf("unexpected error describing all: %v", err)
	}
	if sm, ok := res.(*SemanticMap); !ok || len(sm.Entities) != 1 {
		t.Errorf("expected 1 entity in full describe, got %v", res)
	}

	// 2. Describe specific entity with case-insensitivity
	resEnt, err := cur.Describe("entities", "order")
	if err != nil {
		t.Fatalf("failed to find entity with case-insensitive name: %v", err)
	}
	entDef, ok := resEnt.(*EntityDef)
	if !ok || entDef.Name != "Order" {
		t.Errorf("expected Order entity, got %v", resEnt)
	}

	// 3. Describe specific machine
	resMach, err := cur.Describe("machines", "orderfsm")
	if err != nil {
		t.Fatalf("failed to find machine: %v", err)
	}
	machDef, ok := resMach.(*MachineDef)
	if !ok || machDef.ID != "OrderFSM" {
		t.Errorf("expected OrderFSM, got %v", resMach)
	}

	// 4. Describe unknown entity
	_, err = cur.Describe("entities", "nonexistent")
	if err == nil {
		t.Errorf("expected error for nonexistent entity, got nil")
	}
}
