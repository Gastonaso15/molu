package issues_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/publisher/issues"
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
