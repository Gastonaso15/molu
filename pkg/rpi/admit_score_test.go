package rpi

import (
	"testing"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

func TestAdmitScoreTypes(t *testing.T) {
	// Test que los tipos y interfaces compilan y tienen la estructura esperada

	// Stage constants
	var _ Stage = StageAdmit
	var _ Stage = StageScore

	// AdmitQuery
	q := AdmitQuery{
		Tenant:        "tenant1",
		Role:          "role1",
		OriginalQuery: "test query",
	}
	if q.Tenant != "tenant1" {
		t.Errorf("AdmitQuery.Tenant = %q, want tenant1", q.Tenant)
	}

	// ScoreQuery embeds AdmitQuery
	sq := ScoreQuery{AdmitQuery: q}
	if sq.Tenant != "tenant1" {
		t.Errorf("ScoreQuery.Tenant = %q, want tenant1", sq.Tenant)
	}

	// Candidate
	c := Candidate{ID: "work_orders.CreateOrder", Description: "Create order"}
	if c.ID != "work_orders.CreateOrder" {
		t.Errorf("Candidate.ID = %q, want work_orders.CreateOrder", c.ID)
	}

	// AdmissionResult constants
	var _ AdmissionResult = AdmissionAdmitted
	var _ AdmissionResult = AdmissionNotOffered

	// AdmittedCandidate
	ac := AdmittedCandidate{
		Candidate: c,
		Result:    AdmissionAdmitted,
	}
	if ac.Result != AdmissionAdmitted {
		t.Errorf("AdmittedCandidate.Result = %v, want Admitted", ac.Result)
	}

	// AdmittedCandidate with NotOffered
	acNotOffered := AdmittedCandidate{
		Candidate: c,
		Result:    AdmissionNotOffered,
		Reason:    "not allowed for tenant",
	}
	if acNotOffered.Result != AdmissionNotOffered {
		t.Errorf("AdmittedCandidate.Result = %v, want NotOffered", acNotOffered.Result)
	}
	if acNotOffered.Reason != "not allowed for tenant" {
		t.Errorf("AdmittedCandidate.Reason = %q, want 'not allowed for tenant'", acNotOffered.Reason)
	}

	// DiscoveryResult
	dr := DiscoveryResult{
		Offered:    []Candidate{c},
		NotOffered: []AdmittedCandidate{acNotOffered},
	}
	if len(dr.Offered) != 1 {
		t.Errorf("DiscoveryResult.Offered len = %d, want 1", len(dr.Offered))
	}
	if len(dr.NotOffered) != 1 {
		t.Errorf("DiscoveryResult.NotOffered len = %d, want 1", len(dr.NotOffered))
	}

	}

func TestStageBitmask(t *testing.T) {
	// StageAdmit and StageScore should be distinct bits
	if StageAdmit&StageScore != 0 {
		t.Error("StageAdmit and StageScore should not overlap")
	}
	if StageAdmit != 1 {
		t.Errorf("StageAdmit = %d, want 1", StageAdmit)
	}
	if StageScore != 2 {
		t.Errorf("StageScore = %d, want 2", StageScore)
	}

	// Combined
	combined := StageAdmit | StageScore
	if combined != 3 {
		t.Errorf("StageAdmit|StageScore = %d, want 3", combined)
	}
	if combined&StageAdmit == 0 {
		t.Error("combined should have StageAdmit bit")
	}
	if combined&StageScore == 0 {
		t.Error("combined should have StageScore bit")
	}
}

func TestNotOfferedToTaxonomyError(t *testing.T) {
	// Test with NotOffered candidate
	candidate := Candidate{ID: "work_orders.CreateOrder", Description: "Create order"}
	acNotOffered := AdmittedCandidate{
		Candidate: candidate,
		Result:    AdmissionNotOffered,
		Reason:    "not allowed for tenant",
	}

	err := NotOfferedToTaxonomyError("tenant1", acNotOffered)
	if err == nil {
		t.Fatal("expected error for NotOffered candidate")
	}

	if err.Code != string(taxonomy.PrefixMolu+taxonomy.CodeNotOffered) {
		t.Errorf("Code = %q, want %q", err.Code, taxonomy.PrefixMolu+taxonomy.CodeNotOffered)
	}

	// Verify it's NOT NOT_FOUND
	if err.Code == string(taxonomy.PrefixMolu+taxonomy.CodeNotFound) {
		t.Error("code should not be NOT_FOUND")
	}

	// Verify details contain candidate ID (as "tool" key per taxonomy.NewNotOffered)
	if err.Details == nil {
		t.Fatal("Details is nil")
	}
	if err.Details["tool"] != "work_orders.CreateOrder" {
		t.Errorf("Details[tool] = %v, want work_orders.CreateOrder", err.Details["tool"])
	}
	if err.Details["tenant"] != "tenant1" {
		t.Errorf("Details[tenant] = %v, want tenant1", err.Details["tenant"])
	}

	// Test with Admitted candidate - should return nil
	acAdmitted := AdmittedCandidate{
		Candidate: candidate,
		Result:    AdmissionAdmitted,
	}
	err = NotOfferedToTaxonomyError("tenant1", acAdmitted)
	if err != nil {
		t.Errorf("expected nil for Admitted candidate, got %v", err)
	}
}