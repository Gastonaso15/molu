package rpi

import "context"

// Stage: bitmask de etapas que implementa un proveedor
type Stage uint8

const (
	StageAdmit Stage = 1 << iota
	StageScore
)

// AdmitQuery: contexto de quién pregunta
type AdmitQuery struct {
	Tenant        string
	Role          string
	OriginalQuery string
}

// ScoreQuery: contexto de scoring (hereda de AdmitQuery)
type ScoreQuery struct {
	AdmitQuery
}

// Candidate: referencia a función de dominio del catálogo (sin schemas copiados)
type Candidate struct {
	ID          string // namespace.Nombre, ej. "work_orders.CreateOrder"
	Description string // opcional
}

// AdmissionResult: resultado de admisión por candidato
type AdmissionResult uint8

const (
	AdmissionAdmitted AdmissionResult = iota
	AdmissionNotOffered
)

// AdmittedCandidate: Candidate + su resultado de admisión
type AdmittedCandidate struct {
	Candidate
	Result AdmissionResult
	Reason string // solo si NotOffered
}

// DiscoveryResult: resultado final de Discover
type DiscoveryResult struct {
	Offered    []Candidate        // solo admitidos, en orden final (tras Score si aplica)
	NotOffered []AdmittedCandidate // los rechazados con su Reason
}

// Admitter: etapa obligatoria — filtra y marca candidatos
type Admitter interface {
	Admit(ctx context.Context, query AdmitQuery, candidates []Candidate) ([]AdmittedCandidate, error)
	// error solo para SUBSTRATE_UNAVAILABLE del proveedor
}

// Scorer: etapa opcional — reordena admitidos (sin exponer score/rationale)
type Scorer interface {
	Score(ctx context.Context, query ScoreQuery, admitted []AdmittedCandidate) ([]AdmittedCandidate, error)
}

// RPIProvider: proveedor concreto declara qué etapas implementa
type RPIProvider interface {
	Admitter // embebido: Admit es obligatorio
	// Scorer es opcional; si no lo implementa, el registry lo detecta
	StagesImplemented() Stage // bitmask: StageAdmit | StageScore
}