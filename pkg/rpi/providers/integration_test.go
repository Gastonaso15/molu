package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// TestIntegration_Conformity_Section25 verifica las garantías del §2.5 para AMBOS proveedores
func TestIntegration_Conformity_Section25(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		rules    string
	}{
		{
			name:     "admit-all",
			provider: "admit-all",
			rules:    "",
		},
		{
			name:     "static-context-rules",
			provider: "static-context-rules",
			rules:    "testdata/rules.json",
		},
		{
			name:     "static-context-rules-no-rule",
			provider: "static-context-rules",
			rules:    "testdata/rules.json",
		},
	}

	candidates := []rpi.Candidate{
		{ID: "work_orders.CreateOrder", Description: "Create order"},
		{ID: "inventory.Reserve", Description: "Reserve inventory"},
		{ID: "billing.Invoice", Description: "Create invoice"},
	}

	query := rpi.AdmitQuery{Tenant: "tenant1", Role: "admin", OriginalQuery: "test"}
	noRuleQuery := rpi.AdmitQuery{Tenant: "tenant999", Role: "rolenoexiste", OriginalQuery: "test"}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewProviderRegistry()
			provider, err := reg.Create(tc.provider, ProviderConfig{RulesFile: tc.rules})
			if err != nil {
				t.Fatalf("Create error: %v", err)
			}

			q := query
			if tc.name == "static-context-rules-no-rule" {
				q = noRuleQuery
			}

			result, err := rpi.Discover(context.Background(), provider, q, candidates)
			if err != nil {
				t.Fatalf("Discover error: %v", err)
			}

			// (a) Salida = referencias namespace.Nombre (no copias)
			for _, offered := range result.Offered {
				if offered.ID == "" {
					t.Errorf("Offered candidate has empty ID")
				}
				// Verificar formato namespace.Nombre
				if offered.ID == "work_orders.CreateOrder" || offered.ID == "inventory.Reserve" || offered.ID == "billing.Invoice" {
					// OK, formato correcto
				} else {
					t.Errorf("Offered candidate ID %q doesn't match namespace.Nombre pattern", offered.ID)
				}
			}

			// (b) Vacío honesto sin padding
			// Caso especial: (tenant, rol) sin regla -> Offered vacío, err == nil, todos en NotOffered con Reason
			if tc.name == "static-context-rules-no-rule" {
				if len(result.Offered) != 0 {
					t.Errorf("Offered should be empty for tenant/role without rule, got %d", len(result.Offered))
				}
				if err != nil {
					t.Errorf("err should be nil for tenant/role without rule, got %v", err)
				}
				if len(result.NotOffered) != len(candidates) {
					t.Errorf("NotOffered should have all %d candidates, got %d", len(candidates), len(result.NotOffered))
				}
				for _, no := range result.NotOffered {
					if no.Reason == "" {
						t.Errorf("NotOffered %q should have non-empty Reason", no.Candidate.ID)
					}
				}
			} else if len(result.Offered) == 0 {
				// EMPTY válido - no error, no padding
				if result.Offered == nil {
					// nil slice es OK para empty
				}
			}

			// (c) Rechazados → NOT_OFFERED con Reason
			for _, notOffered := range result.NotOffered {
				if notOffered.Result != rpi.AdmissionNotOffered {
					t.Errorf("NotOffered candidate %q has Result=%v, want NotOffered", notOffered.Candidate.ID, notOffered.Result)
				}
				if notOffered.Reason == "" {
					t.Errorf("NotOffered candidate %q has empty Reason", notOffered.Candidate.ID)
				}
			}

			// (d) Proveedor falso → SUBSTRATE_UNAVAILABLE tipado (no lista vacía)
			// Este se prueba en TestIntegration_SUBSTRATE_UNAVAILABLE_Typed

			// (e) Offered sin score/rationale
			for _, offered := range result.Offered {
				// Candidate no tiene campos score/rationale - verificado por compilación
				_ = offered
			}
		})
	}
}

// TestIntegration_Criterion7_ProviderSwitch: mismo contexto y candidatos, admit-all vs static-context-rules = outputs distintos
func TestIntegration_Criterion7_ProviderSwitch(t *testing.T) {
	candidates := []rpi.Candidate{
		{ID: "work_orders.CreateOrder", Description: "Create order"},
		{ID: "inventory.Reserve", Description: "Reserve inventory"},
		{ID: "billing.Invoice", Description: "Create invoice"},
	}

	query := rpi.AdmitQuery{Tenant: "tenant1", Role: "admin", OriginalQuery: "test"}

	reg := NewProviderRegistry()

	// admit-all: admite todos
	admitAllProvider, err := reg.Create("admit-all", ProviderConfig{})
	if err != nil {
		t.Fatalf("admit-all Create error: %v", err)
	}

	admitAllResult, err := rpi.Discover(context.Background(), admitAllProvider, query, candidates)
	if err != nil {
		t.Fatalf("admit-all Discover error: %v", err)
	}

	// static-context-rules: filtra según reglas (tenant1/admin permite CreateOrder y Reserve)
	staticProvider, err := reg.Create("static-context-rules", ProviderConfig{RulesFile: "testdata/rules.json"})
	if err != nil {
		t.Fatalf("static-context-rules Create error: %v", err)
	}

	staticResult, err := rpi.Discover(context.Background(), staticProvider, query, candidates)
	if err != nil {
		t.Fatalf("static-context-rules Discover error: %v", err)
	}

	// Verificar que son DISTINTOS
	if len(admitAllResult.Offered) == len(staticResult.Offered) {
		// Podrían tener misma longitud pero diferente contenido, verificar IDs
		admitAllIDs := getCandidateIDs(admitAllResult.Offered)
		staticIDs := getCandidateIDs(staticResult.Offered)

		// admit-all debe tener 3, static-context-rules debe tener 2 (Invoice filtrado)
		if len(admitAllIDs) != 3 {
			t.Errorf("admit-all Offered count = %d, want 3", len(admitAllIDs))
		}
		if len(staticIDs) != 2 {
			t.Errorf("static-context-rules Offered count = %d, want 2", len(staticIDs))
		}

		// Verificar que admit-all tiene Invoice y static no
		hasInvoiceAdmitAll := false
		hasInvoiceStatic := false
		for _, id := range admitAllIDs {
			if id == "billing.Invoice" {
				hasInvoiceAdmitAll = true
			}
		}
		for _, id := range staticIDs {
			if id == "billing.Invoice" {
				hasInvoiceStatic = true
			}
		}

		if !hasInvoiceAdmitAll {
			t.Error("admit-all should have billing.Invoice")
		}
		if hasInvoiceStatic {
			t.Error("static-context-rules should NOT have billing.Invoice")
		}
	}
}

// TestIntegration_NOT_OFFERED_Explicit: candidato filtrado → NotOffered con Reason (no eliminado)
func TestIntegration_NOT_OFFERED_Explicit(t *testing.T) {
	reg := NewProviderRegistry()
	provider, err := reg.Create("static-context-rules", ProviderConfig{RulesFile: "testdata/rules.json"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	candidates := []rpi.Candidate{
		{ID: "work_orders.CreateOrder", Description: "Create order"},
		{ID: "billing.Invoice", Description: "Create invoice"}, // NO permitido para tenant1/admin
	}

	query := rpi.AdmitQuery{Tenant: "tenant1", Role: "admin", OriginalQuery: "test"}
	result, err := rpi.Discover(context.Background(), provider, query, candidates)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}

	// Debe haber 1 Offered y 1 NotOffered
	if len(result.Offered) != 1 {
		t.Errorf("Offered count = %d, want 1", len(result.Offered))
	}
	if len(result.NotOffered) != 1 {
		t.Errorf("NotOffered count = %d, want 1", len(result.NotOffered))
	}

	// El NotOffered debe ser billing.Invoice con Reason no vacío
	no := result.NotOffered[0]
	if no.Candidate.ID != "billing.Invoice" {
		t.Errorf("NotOffered ID = %q, want billing.Invoice", no.Candidate.ID)
	}
	if no.Reason == "" {
		t.Error("NotOffered Reason should not be empty")
	}

	// Verificar conversión a error tipado de taxonomía: NOT_OFFERED exacto, no NOT_FOUND
	taxErr := rpi.NotOfferedToTaxonomyError(query.Tenant, no)
	if taxErr == nil {
		t.Fatal("expected taxonomy error for NotOffered candidate")
	}
	if taxErr.Code != string(taxonomy.PrefixMolu+taxonomy.CodeNotOffered) {
		t.Errorf("Code = %q, want %q", taxErr.Code, taxonomy.PrefixMolu+taxonomy.CodeNotOffered)
	}
	// Verificar que NO es NOT_FOUND
	if taxErr.Code == string(taxonomy.PrefixMolu+taxonomy.CodeNotFound) {
		t.Error("code should not be NOT_FOUND")
	}
	// Verificar detalles
	if taxErr.Details == nil {
		t.Fatal("Details is nil")
	}
	if taxErr.Details["tool"] != "billing.Invoice" {
		t.Errorf("Details[tool] = %v, want billing.Invoice", taxErr.Details["tool"])
	}
	if taxErr.Details["tenant"] != "tenant1" {
		t.Errorf("Details[tenant] = %v, want tenant1", taxErr.Details["tenant"])
	}
}

// TestIntegration_EMPTY_ValidResult: ningún admitido → Offered vacío, no error, no SUBSTRATE_UNAVAILABLE
func TestIntegration_EMPTY_ValidResult(t *testing.T) {
	reg := NewProviderRegistry()
	provider, err := reg.Create("static-context-rules", ProviderConfig{RulesFile: "testdata/rules.json"})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Tenant/rol que no tiene regla -> todos NotOffered
	candidates := []rpi.Candidate{
		{ID: "work_orders.CreateOrder", Description: "Create order"},
	}
	query := rpi.AdmitQuery{Tenant: "tenant999", Role: "rolenoexiste", OriginalQuery: "test"}

	result, err := rpi.Discover(context.Background(), provider, query, candidates)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}

	if len(result.Offered) != 0 {
		t.Errorf("Offered count = %d, want 0", len(result.Offered))
	}
	if len(result.NotOffered) != 1 {
		t.Errorf("NotOffered count = %d, want 1", len(result.NotOffered))
	}

	// No error, resultado válido EMPTY
}

// TestIntegration_SUBSTRATE_UNAVAILABLE_Typed: proveedor falso → SUBSTRATE_UNAVAILABLE tipado con component=rpi-provider
func TestIntegration_SUBSTRATE_UNAVAILABLE_Typed(t *testing.T) {
	// Proveedor que siempre falla
	failingProvider := &failingProviderForIntegration{err: errors.New("provider down")}

	candidates := []rpi.Candidate{{ID: "a", Description: "A"}}
	query := rpi.AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	result, err := rpi.Discover(context.Background(), failingProvider, query, candidates)
	if err == nil {
		t.Fatal("expected error from failing provider")
	}

	// Verificar que result es zero value
	if result.Offered != nil || result.NotOffered != nil {
		t.Error("result should be zero value on error")
	}

	// Verificar TypedError con SUBSTRATE_UNAVAILABLE y component=rpi-provider
	var typedErr *taxonomy.TypedError
	if !errors.As(err, &typedErr) {
		t.Fatalf("error is not *taxonomy.TypedError: %T", err)
	}
	if typedErr.Code != string(taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("Code = %v, want %s", typedErr.Code, taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable)
	}
	if typedErr.Details == nil {
		t.Fatal("Details is nil")
	}
	if typedErr.Details["component"] != "rpi-provider" {
		t.Errorf("Details[component] = %v, want 'rpi-provider'", typedErr.Details["component"])
	}
}

// failingProviderForIntegration: proveedor que siempre falla en Admit
type failingProviderForIntegration struct {
	err error
}

func (f *failingProviderForIntegration) StagesImplemented() rpi.Stage {
	return rpi.StageAdmit
}

func (f *failingProviderForIntegration) Admit(ctx context.Context, query rpi.AdmitQuery, candidates []rpi.Candidate) ([]rpi.AdmittedCandidate, error) {
	return nil, f.err
}

// Helper
func getCandidateIDs(candidates []rpi.Candidate) []string {
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ID
	}
	return ids
}