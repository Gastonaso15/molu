package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

type failingScorer struct{}

func (f *failingScorer) Score(ctx context.Context, query rpi.ScoreQuery, admitted []rpi.AdmittedCandidate) ([]rpi.AdmittedCandidate, error) {
	return nil, errors.New("provider unavailable")
}

func TestScoreTestDouble_Score_Reorders(t *testing.T) {
	td := NewScoreTestDouble()

	// Verificar que implementa Scorer
	var _ rpi.Scorer = td

	admitted := []rpi.AdmittedCandidate{
		{Candidate: rpi.Candidate{ID: "a", Description: "A"}, Result: rpi.AdmissionAdmitted},
		{Candidate: rpi.Candidate{ID: "b", Description: "B"}, Result: rpi.AdmissionAdmitted},
		{Candidate: rpi.Candidate{ID: "c", Description: "C"}, Result: rpi.AdmissionAdmitted},
	}

	query := rpi.ScoreQuery{AdmitQuery: rpi.AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}}

	reordered, err := td.Score(context.Background(), query, admitted)
	if err != nil {
		t.Fatalf("Score error: %v", err)
	}

	if len(reordered) != 3 {
		t.Errorf("reordered len = %d, want 3", len(reordered))
	}

	// Verificar que son los mismos candidatos (no copias, referencias)
	// El test double puede reordenar de forma determinista (ej. reverse)
	// Lo importante: mismos IDs, misma cantidad
	ids := make([]string, len(reordered))
	for i, a := range reordered {
		ids[i] = a.Candidate.ID
	}

	// Debe contener los mismos 3 IDs
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["a"] || !found["b"] || !found["c"] {
		t.Errorf("reordered missing some IDs: %v", ids)
	}
}

func TestScoreTestDouble_Score_EmptyAdmitted(t *testing.T) {
	td := NewScoreTestDouble()

	query := rpi.ScoreQuery{AdmitQuery: rpi.AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}}
	reordered, err := td.Score(context.Background(), query, nil)
	if err != nil {
		t.Fatalf("Score error: %v", err)
	}

	if len(reordered) != 0 {
		t.Errorf("reordered len = %d, want 0", len(reordered))
	}
}

func TestScoreTestDouble_Score_DoesNotTouchOtherTenant(t *testing.T) {
	td := NewScoreTestDouble()

	admitted := []rpi.AdmittedCandidate{
		{Candidate: rpi.Candidate{ID: "a", Description: "A"}, Result: rpi.AdmissionAdmitted},
	}

	// ScoreQuery con tenant diferente al de los candidatos (simulando partición)
	query := rpi.ScoreQuery{AdmitQuery: rpi.AdmitQuery{Tenant: "other-tenant", Role: "r", OriginalQuery: "q"}}

	reordered, err := td.Score(context.Background(), query, admitted)
	if err != nil {
		t.Fatalf("Score error: %v", err)
	}

	// Debe devolver los mismos admitidos sin modificar (no puntúa candidatos de otro tenant)
	if len(reordered) != 1 {
		t.Errorf("reordered len = %d, want 1", len(reordered))
	}
	if reordered[0].Candidate.ID != "a" {
		t.Errorf("reordered[0].ID = %q, want a", reordered[0].Candidate.ID)
	}
}

func TestScoreTestDouble_Score_ErrorControlled(t *testing.T) {
	td := NewScoreTestDouble()

	admitted := []rpi.AdmittedCandidate{
		{Candidate: rpi.Candidate{ID: "a", Description: "A"}, Result: rpi.AdmissionAdmitted},
	}

	query := rpi.ScoreQuery{AdmitQuery: rpi.AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}}

	// Simular error controlado
	td.SetError(errors.New("test error"))

	_, err := td.Score(context.Background(), query, admitted)
	if err == nil {
		t.Fatal("expected error when SetError called")
	}
	if err.Error() != "test error" {
		t.Errorf("error = %q, want 'test error'", err.Error())
	}
}

func TestScoreTestDouble_OptionalAdmitter_Delegates(t *testing.T) {
	td := NewScoreTestDouble()

	// Configurar delegate para Admit
	delegate := &AdmitAllProvider{}
	td.SetAdmitDelegate(delegate)

	candidates := []rpi.Candidate{
		{ID: "a", Description: "A"},
		{ID: "b", Description: "B"},
	}
	query := rpi.AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	admitted, err := td.Admit(context.Background(), query, candidates)
	if err != nil {
		t.Fatalf("Admit error: %v", err)
	}

	if len(admitted) != 2 {
		t.Errorf("admitted len = %d, want 2", len(admitted))
	}
	for _, a := range admitted {
		if a.Result != rpi.AdmissionAdmitted {
			t.Errorf("Result = %v, want Admitted", a.Result)
		}
	}
}

func TestScoreTestDouble_StagesImplemented(t *testing.T) {
	td := NewScoreTestDouble()

	stages := td.StagesImplemented()
	if stages&rpi.StageScore == 0 {
		t.Errorf("StagesImplemented() should have StageScore bit, got %v", stages)
	}
	if stages&rpi.StageAdmit != 0 {
		// Opcional: puede tener Admit si delega
		t.Logf("ScoreTestDouble also declares StageAdmit: %v", stages&rpi.StageAdmit != 0)
	}
}