package providers

import (
	"context"
	"testing"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

func TestAdmitAllProvider_Admit(t *testing.T) {
	p, err := NewAdmitAllProvider(ProviderConfig{})
	if err != nil {
		t.Fatalf("NewAdmitAllProvider error: %v", err)
	}

	// Verificar que implementa RPIProvider
	var _ rpi.RPIProvider = p

	// Verificar StagesImplemented
	if p.StagesImplemented() != rpi.StageAdmit {
		t.Errorf("StagesImplemented() = %v, want StageAdmit", p.StagesImplemented())
	}

	// Candidatos de prueba
	candidates := []rpi.Candidate{
		{ID: "work_orders.CreateOrder", Description: "Create order"},
		{ID: "inventory.Reserve", Description: "Reserve inventory"},
		{ID: "billing.Invoice", Description: "Create invoice"},
	}

	query := rpi.AdmitQuery{
		Tenant:        "tenant1",
		Role:          "role1",
		OriginalQuery: "test",
	}

	admitted, err := p.Admit(context.Background(), query, candidates)
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}

	// Debe admitir todos
	if len(admitted) != 3 {
		t.Errorf("admitted len = %d, want 3", len(admitted))
	}

	// Orden estable (mismo orden que entrada)
	for i, a := range admitted {
		if a.Candidate.ID != candidates[i].ID {
			t.Errorf("admitted[%d].ID = %q, want %q (orden estable)", i, a.Candidate.ID, candidates[i].ID)
		}
		if a.Result != rpi.AdmissionAdmitted {
			t.Errorf("admitted[%d].Result = %v, want Admitted", i, a.Result)
		}
		if a.Reason != "" {
			t.Errorf("admitted[%d].Reason = %q, want empty", i, a.Reason)
		}
	}
}

func TestAdmitAllProvider_EmptyCatalog(t *testing.T) {
	p, err := NewAdmitAllProvider(ProviderConfig{})
	if err != nil {
		t.Fatalf("NewAdmitAllProvider error: %v", err)
	}

	query := rpi.AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}
	admitted, err := p.Admit(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}

	if len(admitted) != 0 {
		t.Errorf("admitted len = %d, want 0", len(admitted))
	}
}

func TestAdmitAllProvider_FactoryReturnsInterface(t *testing.T) {
	// Verificar que la factory devuelve la interfaz, no el concreto
	provider, err := NewAdmitAllProvider(ProviderConfig{})
	if err != nil {
		t.Fatalf("NewAdmitAllProvider error: %v", err)
	}

	// Debe ser rpi.RPIProvider, no *AdmitAllProvider
	var _ rpi.RPIProvider = provider
	// Si esto compila, la factory devuelve la interfaz correctamente
}