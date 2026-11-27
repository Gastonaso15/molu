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


// TestRealXolu_SeamAlignmentFullLifecycle exercises T6-T12 and T14 end-to-end
// against a live xolu instance (opt-in via MOLU_REAL_XOLU_URL), including:
//   - T7/T14: single seam_issue_lifecycle definition with merge -> duplicate (zero issue_lifecycle)
//   - T8/T9: Alta -> critico (120/1440 min) and Media -> normal (480/4320 min) SLA deadlines
//   - T10: Seam document shape (channel="api", reported_by, assignee, updated_at, no tenant key)
//   - T11: reject requires reason, sends {motivo, actor}, and persists reject_reason
//   - T12: grouped child issues (master_id > 0) rejected with rule="grouped_child"
//   - Interoperability: reading and advancing an issue stored with UI channel="app"
func TestRealXolu_SeamAlignmentFullLifecycle(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("MOLU_REAL_XOLU_URL"), "/")
	if baseURL == "" {
		t.Skip("MOLU_REAL_XOLU_URL not set")
	}

	sub := issues.NewHTTPSubstrate(baseURL, os.Getenv("MOLU_REAL_XOLU_TOKEN"), 10*time.Second)
	svc := issues.NewIssueService(sub)
	ctx := context.Background()

	// Seed Alta and Media assets in real xolu.
	altaAssetID, err := sub.CreateEntity(ctx, "dev", "assets", map[string]any{
		"name":        "Turbina Principal Real",
		"criticality": "Alta",
		"status":      "active",
	})
	if err != nil {
		t.Fatalf("seed Alta asset: %v", err)
	}
	mediaAssetID, err := sub.CreateEntity(ctx, "dev", "assets", map[string]any{
		"name":        "Bomba Auxiliar Real",
		"criticality": "Media",
		"status":      "active",
	})
	if err != nil {
		t.Fatalf("seed Media asset: %v", err)
	}

	// 1. T8 + T9 + T10: Alta/high issue with reporter_id.
	altaIssue, err := svc.CreateIssue(ctx, issues.CreateIssueRequest{
		Tenant:      "dev",
		Title:       "Vibracion alta en turbina",
		Description: "Detectada en inspeccion",
		Priority:    "high",
		Severity:    "major",
		AssetID:     altaAssetID,
		ReporterID:  77,
	})
	if err != nil {
		t.Fatalf("CreateIssue(Alta): %v", err)
	}
	if altaIssue.AssetCategory != "critico" || altaIssue.Channel != "api" || altaIssue.ReportedBy != 77 || altaIssue.Assignee != 77 {
		t.Fatalf("unexpected Alta issue fields: %+v", altaIssue)
	}
	createdAt, err := time.Parse(time.RFC3339, altaIssue.CreatedAt)
	if err != nil {
		t.Fatalf("parse CreatedAt: %v", err)
	}
	firstDL, err := time.Parse(time.RFC3339, altaIssue.SLAFirstResponseDeadline)
	if err != nil {
		t.Fatalf("parse SLAFirstResponseDeadline: %v", err)
	}
	resDL, err := time.Parse(time.RFC3339, altaIssue.SLAResolutionDeadline)
	if err != nil {
		t.Fatalf("parse SLAResolutionDeadline: %v", err)
	}
	if got := firstDL.Sub(createdAt); got != 120*time.Minute {
		t.Errorf("Alta/high first response SLA = %v, want 120m", got)
	}
	if got := resDL.Sub(createdAt); got != 1440*time.Minute {
		t.Errorf("Alta/high resolution SLA = %v, want 1440m", got)
	}

	rawAlta, err := sub.GetEntity(ctx, "dev", "issues", altaIssue.ID)
	if err != nil {
		t.Fatalf("GetEntity(altaIssue): %v", err)
	}
	if _, hasTenant := rawAlta["tenant"]; hasTenant {
		t.Errorf("raw xolu issue document must not contain tenant, got %v", rawAlta["tenant"])
	}
	if rawAlta["channel"] != "api" || rawAlta["asset_category"] != "critico" {
		t.Errorf("raw xolu document mismatch: %v", rawAlta)
	}

	// Media/medium issue without reporter_id -> normal (480 / 4320 min) and optional keys omitted.
	mediaIssue, err := svc.CreateIssue(ctx, issues.CreateIssueRequest{
		Tenant:   "dev",
		Title:    "Ruido leve en bomba",
		Priority: "medium",
		Severity: "minor",
		AssetID:  mediaAssetID,
	})
	if err != nil {
		t.Fatalf("CreateIssue(Media): %v", err)
	}
	if mediaIssue.AssetCategory != "normal" {
		t.Errorf("Media AssetCategory = %q, want normal", mediaIssue.AssetCategory)
	}
	mCreated, _ := time.Parse(time.RFC3339, mediaIssue.CreatedAt)
	mFirst, _ := time.Parse(time.RFC3339, mediaIssue.SLAFirstResponseDeadline)
	mRes, _ := time.Parse(time.RFC3339, mediaIssue.SLAResolutionDeadline)
	if mFirst.Sub(mCreated) != 480*time.Minute || mRes.Sub(mCreated) != 4320*time.Minute {
		t.Errorf("Media/medium SLA = (%v, %v), want (480m, 4320m)", mFirst.Sub(mCreated), mRes.Sub(mCreated))
	}

	// 2. T7 + T14: triage -> merge -> duplicate, and single seam_issue_lifecycle definition.
	if _, err := svc.AdvanceIssue(ctx, issues.AdvanceIssueRequest{
		Tenant: "dev", IssueID: mediaIssue.ID, Transition: "triage", ActorID: 77,
	}); err != nil {
		t.Fatalf("AdvanceIssue(triage): %v", err)
	}
	merged, err := svc.AdvanceIssue(ctx, issues.AdvanceIssueRequest{
		Tenant: "dev", IssueID: mediaIssue.ID, Transition: "merge", ActorID: 77,
	})
	if err != nil {
		t.Fatalf("AdvanceIssue(merge): %v", err)
	}
	if merged.NewState != "duplicate" || merged.Issue.UpdatedAt == "" {
		t.Fatalf("expected duplicate with updated_at, got %+v", merged)
	}

	// 3. T11: reject without reason fails; reject with reason persists reject_reason.
	var te *taxonomy.TypedError
	_, err = svc.AdvanceIssue(ctx, issues.AdvanceIssueRequest{
		Tenant: "dev", IssueID: altaIssue.ID, Transition: "reject", Reason: "  ",
	})
	if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeContractViolation) {
		t.Fatalf("expected CONTRACT_VIOLATION on blank reject reason, got %v", err)
	}
	rejected, err := svc.AdvanceIssue(ctx, issues.AdvanceIssueRequest{
		Tenant: "dev", IssueID: altaIssue.ID, Transition: "reject", Reason: " Sensor recalibrado ", ActorID: 77,
	})
	if err != nil {
		t.Fatalf("AdvanceIssue(reject with reason): %v", err)
	}
	if rejected.NewState != "rejected" || rejected.Issue.RejectReason != "Sensor recalibrado" {
		t.Fatalf("unexpected reject result: %+v", rejected)
	}
	gotRejected, err := svc.GetIssue(ctx, issues.GetIssueRequest{Tenant: "dev", IssueID: altaIssue.ID})
	if err != nil || gotRejected.State != "rejected" || gotRejected.RejectReason != "Sensor recalibrado" {
		t.Fatalf("GetIssue after reject: %+v err=%v", gotRejected, err)
	}

	// 4. T12: grouped child (master_id > 0) cannot be transitioned individually.
	childIssue, err := svc.CreateIssue(ctx, issues.CreateIssueRequest{
		Tenant: "dev", Title: "Alerta hija agrupada",
	})
	if err != nil {
		t.Fatalf("CreateIssue(child): %v", err)
	}
	if err := sub.PatchEntity(ctx, "dev", "issues", childIssue.ID, map[string]any{"master_id": mediaIssue.ID}); err != nil {
		t.Fatalf("PatchEntity(master_id): %v", err)
	}
	gotChild, err := svc.GetIssue(ctx, issues.GetIssueRequest{Tenant: "dev", IssueID: childIssue.ID})
	if err != nil || gotChild.MasterID != mediaIssue.ID {
		t.Fatalf("GetIssue(child) MasterID = %d, want %d (err=%v)", gotChild.MasterID, mediaIssue.ID, err)
	}
	_, err = svc.AdvanceIssue(ctx, issues.AdvanceIssueRequest{
		Tenant: "dev", IssueID: childIssue.ID, Transition: "start", ActorID: 77,
	})
	if !errors.As(err, &te) || te.Code != taxonomy.PrefixXolu+string(taxonomy.CodeInvalidTransition) || te.Details["rule"] != "grouped_child" {
		t.Fatalf("expected grouped_child XOLU-INVALID_TRANSITION, got %v", err)
	}

	// 5. Cross-interoperability: issue created externally (channel="app") read and walked to closed.
	defID, err := sub.EnsureMachineDefinition(ctx, "dev")
	if err != nil {
		t.Fatalf("EnsureMachineDefinition: %v", err)
	}
	extMachID, extState, err := sub.CreateMachine(ctx, "dev", defID)
	if err != nil {
		t.Fatalf("CreateMachine: %v", err)
	}
	extIssueID, err := sub.CreateEntity(ctx, "dev", "issues", map[string]any{
		"channel":        "app",
		"title":          "Incidencia creada desde UI",
		"description":    "Flujo cruzado",
		"priority":       "high",
		"severity":       "major",
		"state":          extState,
		"machine_id":     extMachID,
		"asset_id":       altaAssetID,
		"asset_category": "critico",
		"reported_by":    int64(12),
		"assignee":       int64(12),
		"created_at":     time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("seed external UI issue: %v", err)
	}
	gotExt, err := svc.GetIssue(ctx, issues.GetIssueRequest{Tenant: "dev", IssueID: extIssueID})
	if err != nil || gotExt.Channel != "app" || gotExt.Assignee != 12 || gotExt.State != "reported" {
		t.Fatalf("GetIssue(external): %+v err=%v", gotExt, err)
	}
	for _, step := range []string{"triage", "start", "resolve", "close"} {
		if _, err := svc.AdvanceIssue(ctx, issues.AdvanceIssueRequest{
			Tenant: "dev", IssueID: extIssueID, Transition: step, ActorID: 25,
		}); err != nil {
			t.Fatalf("AdvanceIssue(external %s): %v", step, err)
		}
	}
	finalExt, err := svc.GetIssue(ctx, issues.GetIssueRequest{Tenant: "dev", IssueID: extIssueID})
	if err != nil || finalExt.State != "closed" || finalExt.FirstResponseAt == "" || finalExt.FirstRespondedBy != 25 || finalExt.UpdatedAt == "" {
		t.Fatalf("final external issue mismatch: %+v err=%v", finalExt, err)
	}
}
