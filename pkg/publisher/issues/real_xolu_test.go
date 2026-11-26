package issues_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/publisher/issues"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// TestRealXolu_SingleDefinition runs only when MOLU_REAL_XOLU_URL is set.
// It creates two issues in sequence and verifies that xolu stores only one
// FSM definition for issues.DefinitionName.
func TestRealXolu_SingleDefinition(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("MOLU_REAL_XOLU_URL"), "/")
	if baseURL == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}

	sub := issues.NewHTTPSubstrate(baseURL, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 10*time.Second)
	svc := issues.NewIssueService(sub)
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		if _, err := svc.CreateIssue(ctx, issues.CreateIssueRequest{
			Tenant: "dev",
			Title:  "Real xolu single definition check",
		}); err != nil {
			t.Fatalf("CreateIssue #%d: %v", i, err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v2/fsm/def", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tok := os.Getenv("MOLU_REAL_XOLU_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET /api/v2/fsm/def: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Definitions []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"definitions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode definitions: %v", err)
	}

	matches := 0
	for _, d := range body.Definitions {
		if d.Name == issues.DefinitionName {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("expected exactly 1 FSM definition named %q in real xolu, got %d (total=%d)",
			issues.DefinitionName, matches, len(body.Definitions))
	}
}

func TestRealXolu_InvalidTransition(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("MOLU_REAL_XOLU_URL"), "/")
	if baseURL == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}

	sub := issues.NewHTTPSubstrate(baseURL, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 10*time.Second)
	svc := issues.NewIssueService(sub)
	ctx := context.Background()

	created, err := svc.CreateIssue(ctx, issues.CreateIssueRequest{
		Tenant: "dev",
		Title:  "Real xolu invalid transition check",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	if _, err := svc.AdvanceIssue(ctx, issues.AdvanceIssueRequest{
		Tenant:     "dev",
		IssueID:    created.ID,
		Transition: "start",
		ActorID:    1,
	}); err != nil {
		t.Fatalf("AdvanceIssue(start): %v", err)
	}

	_, err = svc.AdvanceIssue(ctx, issues.AdvanceIssueRequest{
		Tenant:     "dev",
		IssueID:    created.ID,
		Transition: "close",
		ActorID:    1,
	})
	if err == nil {
		t.Fatal("expected error closing from in_progress")
	}
	var te *taxonomy.TypedError
	if !errors.As(err, &te) {
		t.Fatalf("expected *taxonomy.TypedError, got %T: %v", err, err)
	}
	if te.Code != taxonomy.PrefixXolu+string(taxonomy.CodeInvalidTransition) {
		t.Fatalf("expected XOLU-INVALID_TRANSITION, got %q (msg=%q details=%v)", te.Code, te.Message, te.Details)
	}
	if xc, _ := te.Details["xolu_code"].(string); !strings.HasPrefix(xc, "XOLU-FSM") {
		t.Errorf("expected details.xolu_code with XOLU-FSM prefix, got %q (details=%v)", xc, te.Details)
	}
	if te.Message == "" {
		t.Errorf("expected verbatim xolu message, got empty")
	}

	// Missing machine must remain NOT_FOUND
	_, err = sub.WalkMachine(ctx, "dev", 99999999, "start", nil)
	if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeNotFound) {
		t.Fatalf("expected NOT_FOUND on missing machine, got %v", err)
	}
}
