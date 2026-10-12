package rpi

import (
	"context"
	"errors"
	"testing"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// Mock provider that fails
type failingProvider struct {
	err error
}

func (f *failingProvider) StagesImplemented() Stage {
	return StageAdmit
}

func (f *failingProvider) Admit(ctx context.Context, query AdmitQuery, candidates []Candidate) ([]AdmittedCandidate, error) {
	return nil, f.err
}

// Mock provider that admits and scores
type scoringProvider struct {
	admitResult []AdmittedCandidate
	admitErr    error
	scoreResult []AdmittedCandidate
	scoreErr    error
}

func (s *scoringProvider) StagesImplemented() Stage {
	return StageAdmit | StageScore
}

func (s *scoringProvider) Admit(ctx context.Context, query AdmitQuery, candidates []Candidate) ([]AdmittedCandidate, error) {
	if s.admitErr != nil {
		return nil, s.admitErr
	}
	return s.admitResult, nil
}

func (s *scoringProvider) Score(ctx context.Context, query ScoreQuery, admitted []AdmittedCandidate) ([]AdmittedCandidate, error) {
	if s.scoreErr != nil {
		return nil, s.scoreErr
	}
	return s.scoreResult, nil
}

// Mock provider that admits but declares StageScore without implementing Scorer
type fakeScoringProvider struct {
	admitResult []AdmittedCandidate
}

func (f *fakeScoringProvider) StagesImplemented() Stage {
	return StageAdmit | StageScore
}

func (f *fakeScoringProvider) Admit(ctx context.Context, query AdmitQuery, candidates []Candidate) ([]AdmittedCandidate, error) {
	return f.admitResult, nil
}
// No implementa Scorer

func TestDiscover_AdmitOnly(t *testing.T) {
	candidates := []Candidate{
		{ID: "a", Description: "A"},
		{ID: "b", Description: "B"},
		{ID: "c", Description: "C"},
	}

	provider := &admitAllForTest{}

	query := AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	result, err := Discover(context.Background(), provider, query, candidates)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}

	// Todos admitidos, mismo orden
	if len(result.Offered) != 3 {
		t.Errorf("Offered len = %d, want 3", len(result.Offered))
	}
	for i, c := range result.Offered {
		if c.ID != candidates[i].ID {
			t.Errorf("Offered[%d].ID = %q, want %q", i, c.ID, candidates[i].ID)
		}
	}
	if len(result.NotOffered) != 0 {
		t.Errorf("NotOffered len = %d, want 0", len(result.NotOffered))
	}
}

func TestDiscover_WithNotOffered(t *testing.T) {
	candidates := []Candidate{
		{ID: "a", Description: "A"},
		{ID: "b", Description: "B"},
		{ID: "c", Description: "C"},
	}

	// Provider that admits only a and c
	provider := &selectiveProvider{
		admitIDs: map[string]bool{"a": true, "c": true},
	}

	query := AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	result, err := Discover(context.Background(), provider, query, candidates)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}

	if len(result.Offered) != 2 {
		t.Errorf("Offered len = %d, want 2", len(result.Offered))
	}
	if len(result.NotOffered) != 1 {
		t.Errorf("NotOffered len = %d, want 1", len(result.NotOffered))
	}

	// Verificar NotOffered tiene Reason
	for _, no := range result.NotOffered {
		if no.Reason == "" {
			t.Errorf("NotOffered %q has empty Reason", no.Candidate.ID)
		}
	}
}

func TestDiscover_WithScorer(t *testing.T) {
	candidates := []Candidate{
		{ID: "a", Description: "A"},
		{ID: "b", Description: "B"},
		{ID: "c", Description: "C"},
	}

	admitResult := []AdmittedCandidate{
		{Candidate: candidates[0], Result: AdmissionAdmitted},
		{Candidate: candidates[1], Result: AdmissionAdmitted},
		{Candidate: candidates[2], Result: AdmissionAdmitted},
	}

	// Scorer reordena (reverse)
	scoreResult := []AdmittedCandidate{
		{Candidate: candidates[2], Result: AdmissionAdmitted},
		{Candidate: candidates[1], Result: AdmissionAdmitted},
		{Candidate: candidates[0], Result: AdmissionAdmitted},
	}

	provider := &scoringProvider{
		admitResult: admitResult,
		scoreResult: scoreResult,
	}

	query := AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	result, err := Discover(context.Background(), provider, query, candidates)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}

	// Offered debe estar en el orden del Score (reverse)
	if len(result.Offered) != 3 {
		t.Errorf("Offered len = %d, want 3", len(result.Offered))
	}
	if result.Offered[0].ID != "c" || result.Offered[1].ID != "b" || result.Offered[2].ID != "a" {
		t.Errorf("Offered order = %v, want [c, b, a]", getIDs(result.Offered))
	}
	if len(result.NotOffered) != 0 {
		t.Errorf("NotOffered len = %d, want 0", len(result.NotOffered))
	}
}

func TestDiscover_ProviderUnavailable_Admit(t *testing.T) {
	candidates := []Candidate{{ID: "a", Description: "A"}}
	provider := &failingProvider{err: errors.New("provider down")}
	query := AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	result, err := Discover(context.Background(), provider, query, candidates)
	if err == nil {
		t.Fatal("expected error")
	}

	if result.Offered != nil || result.NotOffered != nil {
		t.Errorf("result should be zero value on error")
	}

	// Verificar que es TypedError con SUBSTRATE_UNAVAILABLE
	var typedErr *taxonomy.TypedError
	if !errors.As(err, &typedErr) {
		t.Fatalf("error is not *taxonomy.TypedError: %T", err)
	}
	if typedErr.Code != string(taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("Code = %v, want SUBSTRATE_UNAVAILABLE", typedErr.Code)
	}
	// Verificar component = rpi-provider
	if typedErr.Details == nil {
		t.Fatal("Details is nil")
	}
	if typedErr.Details["component"] != "rpi-provider" {
		t.Errorf("Details[component] = %v, want 'rpi-provider'", typedErr.Details["component"])
	}
}

func TestDiscover_ProviderUnavailable_Score(t *testing.T) {
	candidates := []Candidate{{ID: "a", Description: "A"}}
	admitResult := []AdmittedCandidate{{Candidate: candidates[0], Result: AdmissionAdmitted}}

	provider := &scoringProvider{
		admitResult: admitResult,
		scoreErr:    errors.New("scorer down"),
	}
	query := AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	_, err := Discover(context.Background(), provider, query, candidates)
	if err == nil {
		t.Fatal("expected error")
	}

	var typedErr *taxonomy.TypedError
	if !errors.As(err, &typedErr) {
		t.Fatalf("error is not *taxonomy.TypedError: %T", err)
	}
	if typedErr.Code != string(taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("Code = %v, want SUBSTRATE_UNAVAILABLE", typedErr.Code)
	}
	if typedErr.Details["component"] != "rpi-provider" {
		t.Errorf("Details[component] = %v, want 'rpi-provider'", typedErr.Details["component"])
	}
}

func TestDiscover_ScoreNotDeclared_NoCall(t *testing.T) {
	candidates := []Candidate{{ID: "a", Description: "A"}}
	provider := &fakeScoringProvider{ // Declara StageScore pero NO implementa Scorer
		admitResult: []AdmittedCandidate{{Candidate: candidates[0], Result: AdmissionAdmitted}},
	}
	query := AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	_, err := Discover(context.Background(), provider, query, candidates)
	if err == nil {
		t.Fatal("expected error when provider declares StageScore but doesn't implement Scorer")
	}

	// Defense in depth: Discover devuelve error si declara StageScore pero no implementa Scorer
	var typedErr *taxonomy.TypedError
	if !errors.As(err, &typedErr) {
		t.Fatalf("error is not *taxonomy.TypedError: %T", err)
	}
	if typedErr.Code != string(taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("Code = %v, want %s", typedErr.Code, taxonomy.PrefixMolu+taxonomy.CodeSubstrateUnavailable)
	}
}

func TestDiscover_EmptyCatalog(t *testing.T) {
	provider := &admitAllForTest{}
	query := AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	result, err := Discover(context.Background(), provider, query, nil)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}

	if len(result.Offered) != 0 {
		t.Errorf("Offered len = %d, want 0", len(result.Offered))
	}
	if len(result.NotOffered) != 0 {
		t.Errorf("NotOffered len = %d, want 0", len(result.NotOffered))
	}
}

func TestDiscover_ScoreOnlyAdmitted(t *testing.T) {
	candidates := []Candidate{
		{ID: "a", Description: "A"},
		{ID: "b", Description: "B"},
		{ID: "c", Description: "C"},
	}

	admitResult := []AdmittedCandidate{
		{Candidate: candidates[0], Result: AdmissionAdmitted},
		{Candidate: candidates[1], Result: AdmissionNotOffered, Reason: "not allowed"},
		{Candidate: candidates[2], Result: AdmissionAdmitted},
	}

	// Score solo recibe los admitidos (a y c), reordena a [c, a]
	scoreResult := []AdmittedCandidate{
		{Candidate: candidates[2], Result: AdmissionAdmitted},
		{Candidate: candidates[0], Result: AdmissionAdmitted},
	}

	provider := &scoringProvider{
		admitResult: admitResult,
		scoreResult: scoreResult,
	}
	query := AdmitQuery{Tenant: "t", Role: "r", OriginalQuery: "q"}

	result, err := Discover(context.Background(), provider, query, candidates)
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}

	// Offered debe tener solo a y c, en orden del Score
	if len(result.Offered) != 2 {
		t.Errorf("Offered len = %d, want 2", len(result.Offered))
	}
	if result.Offered[0].ID != "c" || result.Offered[1].ID != "a" {
		t.Errorf("Offered = %v, want [c, a]", getIDs(result.Offered))
	}
	// NotOffered debe tener b con Reason
	if len(result.NotOffered) != 1 {
		t.Errorf("NotOffered len = %d, want 1", len(result.NotOffered))
	}
	if result.NotOffered[0].Candidate.ID != "b" {
		t.Errorf("NotOffered[0].ID = %q, want 'b'", result.NotOffered[0].Candidate.ID)
	}
	if result.NotOffered[0].Reason == "" {
		t.Error("NotOffered[0].Reason should not be empty")
	}
}

// Helpers
type admitAllForTest struct{}

func (a *admitAllForTest) StagesImplemented() Stage { return StageAdmit }
func (a *admitAllForTest) Admit(ctx context.Context, query AdmitQuery, candidates []Candidate) ([]AdmittedCandidate, error) {
	out := make([]AdmittedCandidate, len(candidates))
	for i, c := range candidates {
		out[i] = AdmittedCandidate{Candidate: c, Result: AdmissionAdmitted}
	}
	return out, nil
}

type selectiveProvider struct {
	admitIDs map[string]bool
}

func (s *selectiveProvider) StagesImplemented() Stage { return StageAdmit }
func (s *selectiveProvider) Admit(ctx context.Context, query AdmitQuery, candidates []Candidate) ([]AdmittedCandidate, error) {
	out := make([]AdmittedCandidate, len(candidates))
	for i, c := range candidates {
		if s.admitIDs[c.ID] {
			out[i] = AdmittedCandidate{Candidate: c, Result: AdmissionAdmitted}
		} else {
			out[i] = AdmittedCandidate{Candidate: c, Result: AdmissionNotOffered, Reason: "not allowed"}
		}
	}
	return out, nil
}

func getIDs(candidates []Candidate) []string {
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ID
	}
	return ids
}