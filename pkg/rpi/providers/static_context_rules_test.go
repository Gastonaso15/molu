package providers

import (
	"context"
	"os"
	"testing"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

func TestStaticContextRulesProvider_LoadRules(t *testing.T) {
	// Valid JSON
	tmpfile, err := os.CreateTemp("", "rules-*.json")
	if err != nil {
		t.Fatalf("CreateTemp error: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	validJSON := `{
		"version": "1",
		"rules": [
			{"tenant": "tenant1", "role": "admin", "allowedIds": ["work_orders.CreateOrder", "inventory.Reserve"]},
			{"tenant": "tenant1", "role": "user", "allowedIds": ["work_orders.CreateOrder"]},
			{"tenant": "tenant2", "role": "admin", "allowedIds": ["billing.Invoice"]}
		]
	}`
	if _, err := tmpfile.WriteString(validJSON); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	tmpfile.Close()

	p, err := NewStaticContextRulesProvider(ProviderConfig{RulesFile: tmpfile.Name()})
	if err != nil {
		t.Fatalf("NewStaticContextRulesProvider error: %v", err)
	}

	// Verify StagesImplemented
	if p.StagesImplemented() != rpi.StageAdmit {
		t.Errorf("StagesImplemented() = %v, want StageAdmit", p.StagesImplemented())
	}

	// Verify factory returns interface
	var _ rpi.RPIProvider = p
}

func TestStaticContextRulesProvider_LoadRules_InvalidJSON(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "rules-*.json")
	if err != nil {
		t.Fatalf("CreateTemp error: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(`{ invalid json }`); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	tmpfile.Close()

	_, err = NewStaticContextRulesProvider(ProviderConfig{RulesFile: tmpfile.Name()})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestStaticContextRulesProvider_LoadRules_MissingFile(t *testing.T) {
	_, err := NewStaticContextRulesProvider(ProviderConfig{RulesFile: "/nonexistent/rules.json"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestStaticContextRulesProvider_Admit_WithRule(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "rules-*.json")
	if err != nil {
		t.Fatalf("CreateTemp error: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	validJSON := `{
		"version": "1",
		"rules": [
			{"tenant": "tenant1", "role": "admin", "allowedIds": ["work_orders.CreateOrder", "inventory.Reserve"]},
			{"tenant": "tenant1", "role": "user", "allowedIds": ["work_orders.CreateOrder"]}
		]
	}`
	if _, err := tmpfile.WriteString(validJSON); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	tmpfile.Close()

	p, err := NewStaticContextRulesProvider(ProviderConfig{RulesFile: tmpfile.Name()})
	if err != nil {
		t.Fatalf("NewStaticContextRulesProvider error: %v", err)
	}

	candidates := []rpi.Candidate{
		{ID: "work_orders.CreateOrder", Description: "Create order"},
		{ID: "inventory.Reserve", Description: "Reserve inventory"},
		{ID: "billing.Invoice", Description: "Create invoice"},
	}

	// Tenant1 + admin: should admit CreateOrder and Reserve, reject Invoice
	query := rpi.AdmitQuery{Tenant: "tenant1", Role: "admin", OriginalQuery: "test"}
	admitted, err := p.Admit(context.Background(), query, candidates)
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}

	if len(admitted) != 3 {
		t.Errorf("admitted len = %d, want 3", len(admitted))
	}

	// Verify expected results
	expected := map[string]rpi.AdmissionResult{
		"work_orders.CreateOrder": rpi.AdmissionAdmitted,
		"inventory.Reserve":       rpi.AdmissionAdmitted,
		"billing.Invoice":         rpi.AdmissionNotOffered,
	}

	for _, a := range admitted {
		exp, ok := expected[a.Candidate.ID]
		if !ok {
			t.Errorf("unexpected candidate ID: %q", a.Candidate.ID)
			continue
		}
		if a.Result != exp {
			t.Errorf("%s: Result = %v, want %v", a.Candidate.ID, a.Result, exp)
		}
		if a.Result == rpi.AdmissionNotOffered && a.Reason == "" {
			t.Errorf("%s: Reason should not be empty for NotOffered", a.Candidate.ID)
		}
		if a.Result == rpi.AdmissionAdmitted && a.Reason != "" {
			t.Errorf("%s: Reason should be empty for Admitted, got %q", a.Candidate.ID, a.Reason)
		}
	}
}

func TestStaticContextRulesProvider_Admit_NoRule(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "rules-*.json")
	if err != nil {
		t.Fatalf("CreateTemp error: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	validJSON := `{
		"version": "1",
		"rules": [
			{"tenant": "tenant1", "role": "admin", "allowedIds": ["work_orders.CreateOrder"]}
		]
	}`
	if _, err := tmpfile.WriteString(validJSON); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	tmpfile.Close()

	p, err := NewStaticContextRulesProvider(ProviderConfig{RulesFile: tmpfile.Name()})
	if err != nil {
		t.Fatalf("NewStaticContextRulesProvider error: %v", err)
	}

	candidates := []rpi.Candidate{
		{ID: "work_orders.CreateOrder", Description: "Create order"},
		{ID: "inventory.Reserve", Description: "Reserve inventory"},
	}

	// Tenant1 + user: no rule for (tenant1, user) -> deny by default
	query := rpi.AdmitQuery{Tenant: "tenant1", Role: "user", OriginalQuery: "test"}
	admitted, err := p.Admit(context.Background(), query, candidates)
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}

	if len(admitted) != 2 {
		t.Errorf("admitted len = %d, want 2", len(admitted))
	}

	for _, a := range admitted {
		if a.Result != rpi.AdmissionNotOffered {
			t.Errorf("%s: Result = %v, want NotOffered (deny by default)", a.Candidate.ID, a.Result)
		}
		if a.Reason == "" {
			t.Errorf("%s: Reason should not be empty for NotOffered", a.Candidate.ID)
		}
	}
}

func TestStaticContextRulesProvider_Admit_EmptyCatalog(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "rules-*.json")
	if err != nil {
		t.Fatalf("CreateTemp error: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	validJSON := `{"version": "1", "rules": []}`
	if _, err := tmpfile.WriteString(validJSON); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	tmpfile.Close()

	p, err := NewStaticContextRulesProvider(ProviderConfig{RulesFile: tmpfile.Name()})
	if err != nil {
		t.Fatalf("NewStaticContextRulesProvider error: %v", err)
	}

	query := rpi.AdmitQuery{Tenant: "tenant1", Role: "admin", OriginalQuery: "test"}
	admitted, err := p.Admit(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}

	if len(admitted) != 0 {
		t.Errorf("admitted len = %d, want 0", len(admitted))
	}
}

func TestStaticContextRulesProvider_Admit_StableOrder(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "rules-*.json")
	if err != nil {
		t.Fatalf("CreateTemp error: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	validJSON := `{
		"version": "1",
		"rules": [
			{"tenant": "tenant1", "role": "admin", "allowedIds": ["a", "b", "c"]}
		]
	}`
	if _, err := tmpfile.WriteString(validJSON); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	tmpfile.Close()

	p, err := NewStaticContextRulesProvider(ProviderConfig{RulesFile: tmpfile.Name()})
	if err != nil {
		t.Fatalf("NewStaticContextRulesProvider error: %v", err)
	}

	candidates := []rpi.Candidate{
		{ID: "c", Description: "C"},
		{ID: "a", Description: "A"},
		{ID: "b", Description: "B"},
	}

	query := rpi.AdmitQuery{Tenant: "tenant1", Role: "admin", OriginalQuery: "test"}
	admitted, err := p.Admit(context.Background(), query, candidates)
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}

	// Stable order = input order (not alphabetical order of allowedIds)
	expectedOrder := []string{"c", "a", "b"}
	for i, a := range admitted {
		if a.Candidate.ID != expectedOrder[i] {
			t.Errorf("admitted[%d].ID = %q, want %q (stable order)", i, a.Candidate.ID, expectedOrder[i])
		}
	}
}

func TestStaticContextRulesProvider_FactoryReturnsInterface(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "rules-*.json")
	if err != nil {
		t.Fatalf("CreateTemp error: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	validJSON := `{"version": "1", "rules": []}`
	if _, err := tmpfile.WriteString(validJSON); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	tmpfile.Close()

	provider, err := NewStaticContextRulesProvider(ProviderConfig{RulesFile: tmpfile.Name()})
	if err != nil {
		t.Fatalf("NewStaticContextRulesProvider error: %v", err)
	}

	var _ rpi.RPIProvider = provider
}