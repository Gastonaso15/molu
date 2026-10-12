package rpi

import (
	"context"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// Stage: bitmask of stages implemented by a provider
type Stage uint8

const (
	StageAdmit Stage = 1 << iota
	StageScore
)

// AdmitQuery: context of who is asking
type AdmitQuery struct {
	Tenant        string
	Role          string
	OriginalQuery string
}

// ScoreQuery: scoring context (inherits from AdmitQuery)
type ScoreQuery struct {
	AdmitQuery
}

// Candidate: reference to a domain function from the catalog (no schemas copied)
type Candidate struct {
	ID          string // namespace.Nombre, e.g. "work_orders.CreateOrder"
	Description string // optional
}

// AdmissionResult: admission result per candidate
type AdmissionResult uint8

const (
	AdmissionAdmitted AdmissionResult = iota
	AdmissionNotOffered
)

// AdmittedCandidate: Candidate + its admission result
type AdmittedCandidate struct {
	Candidate
	Result AdmissionResult
	Reason string // only if NotOffered
}

// DiscoveryResult: final result of Discover
type DiscoveryResult struct {
	Offered    []Candidate        // only admitted, in final order (after Score if applicable)
	NotOffered []AdmittedCandidate // rejected candidates with their Reason
}

// Admitter: mandatory stage - filters and marks candidates
type Admitter interface {
	Admit(ctx context.Context, query AdmitQuery, candidates []Candidate) ([]AdmittedCandidate, error)
	// error only for SUBSTRATE_UNAVAILABLE from provider
}

// Scorer: optional stage - reorders admitted candidates (without exposing score/rationale)
type Scorer interface {
	Score(ctx context.Context, query ScoreQuery, admitted []AdmittedCandidate) ([]AdmittedCandidate, error)
}

// RPIProvider: concrete provider declares which stages it implements
type RPIProvider interface {
	Admitter // embedded: Admit is mandatory
	// Scorer is optional; if not implemented, registry detects it
	StagesImplemented() Stage // bitmask: StageAdmit | StageScore
}

// NotOfferedToTaxonomyError: converts a NotOffered AdmittedCandidate to a taxonomy typed error
func NotOfferedToTaxonomyError(tenant string, ac AdmittedCandidate) *taxonomy.TypedError {
	if ac.Result != AdmissionNotOffered {
		return nil
	}
	return taxonomy.NewNotOffered(tenant, ac.Candidate.ID)
}