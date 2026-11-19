package rpi_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// scoringOnlyDouble is a minimal test double simulating an external semantic
// ranking engine plugged exclusively into the Score stage (§2.5). It declares
// Capabilities{Admit: false, Score: true} and keeps its internal ranking state
// completely hidden behind the RPIProvider contract.
type scoringOnlyDouble struct {
	available bool
	// internalWeights simulates private vector/semantic scores that never leak
	// outside the provider interface.
	internalWeights map[string]float64
	logger          telemetry.Logger
	metrics         telemetry.Metrics
}

var _ rpi.RPIProvider = (*scoringOnlyDouble)(nil)

func (s *scoringOnlyDouble) Capabilities() rpi.Capabilities {
	return rpi.Capabilities{Admit: false, Score: true}
}

func (s *scoringOnlyDouble) Admit(ctx context.Context, actx rpi.AdmitContext, candidates []rpi.ToolCandidate) ([]rpi.ToolCandidate, error) {
	// A score-only engine does not perform tenant/role admission itself.
	out := make([]rpi.ToolCandidate, len(candidates))
	copy(out, candidates)
	return out, nil
}

func (s *scoringOnlyDouble) Score(ctx context.Context, actx rpi.AdmitContext, admitted []rpi.ToolCandidate) ([]rpi.ScoredTool, error) {
	if !s.available {
		return nil, taxonomy.NewSubstrateUnavailable(
			taxonomy.NowRFC3339(),
			taxonomy.NextRetryRFC3339(1, 500*time.Millisecond, 5*time.Second),
			1,
			3,
		)
	}
	// Guarantee: never pad empty results.
	if len(admitted) == 0 {
		return []rpi.ScoredTool{}, nil
	}

	scored := make([]rpi.ScoredTool, len(admitted))
	for i, cand := range admitted {
		score := 0.1
		if w, ok := s.internalWeights[cand.Name]; ok {
			score = w
		} else if actx.Query != "" && strings.Contains(strings.ToLower(cand.Description), strings.ToLower(actx.Query)) {
			score = 0.9
		}
		scored[i] = rpi.ScoredTool{
			ToolCandidate: cand,
			Score:         score,
		}
	}

	slices.SortStableFunc(scored, func(a, b rpi.ScoredTool) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return scored, nil
}

func (s *scoringOnlyDouble) SetLogger(l telemetry.Logger)   { s.logger = l }
func (s *scoringOnlyDouble) SetMetrics(m telemetry.Metrics) { s.metrics = m }
func (s *scoringOnlyDouble) Logger() telemetry.Logger       { return s.logger }

func TestCapabilities_V1ProvidersAndScoreDouble(t *testing.T) {
	admitAll := providers.NewAdmitAllProvider()
	if got := admitAll.Capabilities(); got != (rpi.Capabilities{Admit: true, Score: false}) {
		t.Fatalf("admit-all Capabilities() = %+v, want {Admit:true Score:false}", got)
	}

	staticProv := providers.NewStaticContextRulesProvider(map[string][]string{
		"acme:op": {"work_orders.CreateOrder"},
	})
	if got := staticProv.Capabilities(); got != (rpi.Capabilities{Admit: true, Score: false}) {
		t.Fatalf("static-context-rules Capabilities() = %+v, want {Admit:true Score:false}", got)
	}

	scoreDouble := &scoringOnlyDouble{available: true}
	if got := scoreDouble.Capabilities(); got != (rpi.Capabilities{Admit: false, Score: true}) {
		t.Fatalf("scoringOnlyDouble Capabilities() = %+v, want {Admit:false Score:true}", got)
	}
}

func TestScoreOnlyDouble_Guarantees(t *testing.T) {
	ctx := context.Background()

	// Stage 1: Tenant/role partition happens BEFORE scoring (§2.5).
	admitter := providers.NewStaticContextRulesProvider(map[string][]string{
		"tenant-a:tech": {"work_orders.GetOrder", "work_orders.CreateOrder"},
	})
	// Stage 2: Score-only double ranks only what was admitted.
	scorer := &scoringOnlyDouble{
		available: true,
		internalWeights: map[string]float64{
			"work_orders.CreateOrder":  0.95,
			"work_orders.GetOrder":     0.40,
			"tenant_b.SecretOperation": 1.00, // Higher weight, but belongs to another tenant
		},
	}

	catalog := []rpi.ToolCandidate{
		{Name: "work_orders.GetOrder", Namespace: "work_orders", Description: "Read work order"},
		{Name: "tenant_b.SecretOperation", Namespace: "tenant_b", Description: "Other tenant tool"},
		{Name: "work_orders.CreateOrder", Namespace: "work_orders", Description: "Create work order"},
	}

	actx := rpi.AdmitContext{Tenant: "tenant-a", Roles: []string{"tech"}, Query: "create"}
	admitted, err := rpi.AdmitWithLogging(admitter, ctx, actx, catalog)
	if err != nil {
		t.Fatalf("admit error: %v", err)
	}
	scored, err := rpi.ScoreWithLogging(scorer, ctx, actx, admitted)
	if err != nil {
		t.Fatalf("score error: %v", err)
	}

	// Only tenant-a's admitted tools are returned, ordered by descending score.
	if len(scored) != 2 {
		t.Fatalf("expected 2 scored tools (no cross-tenant leakage), got %d: %+v", len(scored), scored)
	}
	if scored[0].Name != "work_orders.CreateOrder" || scored[1].Name != "work_orders.GetOrder" {
		t.Fatalf("unexpected score order: %+v", scored)
	}

	// Empty admitted set -> empty result, never padded.
	emptyScored, err := rpi.ScoreWithLogging(scorer, ctx, rpi.AdmitContext{Tenant: "tenant-unknown"}, []rpi.ToolCandidate{})
	if err != nil {
		t.Fatalf("unexpected error on empty admitted: %v", err)
	}
	if len(emptyScored) != 0 {
		t.Fatalf("expected 0 tools (no padding), got %d", len(emptyScored))
	}

	// Unavailable scoring engine -> typed SUBSTRATE_UNAVAILABLE, never silent empty.
	scorer.available = false
	_, err = rpi.ScoreWithLogging(scorer, ctx, actx, admitted)
	if err == nil {
		t.Fatal("expected SUBSTRATE_UNAVAILABLE error when scorer is down, got nil")
	}
	if !strings.Contains(taxonomy.CodeFromError(err), string(taxonomy.CodeSubstrateUnavailable)) {
		t.Fatalf("expected SUBSTRATE_UNAVAILABLE, got %v", err)
	}
}
