package issues_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Gastonaso15/molu/pkg/publisher/issues"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type mockXoluClient struct {
	mu           sync.Mutex
	entities     map[string]map[int64]map[string]any
	machines     map[int64]*mockMachine
	nextEntityID int64
	nextMachID   int64
	defID        int64

	walkErr      error
	substrateErr error

	walkCalls       int
	lastWalkPayload map[string]any
}

type mockMachine struct {
	id    int64
	state string
}

func newMockXolu() *mockXoluClient {
	return &mockXoluClient{
		entities:     make(map[string]map[int64]map[string]any),
		machines:     make(map[int64]*mockMachine),
		nextEntityID: 100,
		nextMachID:   500,
		defID:        1,
	}
}

func (m *mockXoluClient) GetEntity(ctx context.Context, tenant, entityType string, id int64) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.substrateErr != nil {
		return nil, m.substrateErr
	}
	tEntities, ok := m.entities[entityType]
	if !ok {
		return nil, taxonomy.NewNotFound(tenant, fmt.Sprintf("%s:%d", entityType, id))
	}
	e, ok := tEntities[id]
	if !ok {
		return nil, taxonomy.NewNotFound(tenant, fmt.Sprintf("%s:%d", entityType, id))
	}
	return e, nil
}

func (m *mockXoluClient) CreateEntity(ctx context.Context, tenant, entityType string, data map[string]any) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.substrateErr != nil {
		return 0, m.substrateErr
	}
	m.nextEntityID++
	id := m.nextEntityID
	if m.entities[entityType] == nil {
		m.entities[entityType] = make(map[int64]map[string]any)
	}
	record := make(map[string]any)
	for k, v := range data {
		record[k] = v
	}
	record["id"] = id
	m.entities[entityType][id] = record
	return id, nil
}

func (m *mockXoluClient) PatchEntity(ctx context.Context, tenant, entityType string, id int64, patch map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.substrateErr != nil {
		return m.substrateErr
	}
	tEntities, ok := m.entities[entityType]
	if !ok {
		return taxonomy.NewNotFound(tenant, fmt.Sprintf("%s:%d", entityType, id))
	}
	e, ok := tEntities[id]
	if !ok {
		return taxonomy.NewNotFound(tenant, fmt.Sprintf("%s:%d", entityType, id))
	}
	for k, v := range patch {
		e[k] = v
	}
	return nil
}

func (m *mockXoluClient) EnsureMachineDefinition(ctx context.Context, tenant string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.substrateErr != nil {
		return 0, m.substrateErr
	}
	return m.defID, nil
}

func (m *mockXoluClient) CreateMachine(ctx context.Context, tenant string, defID int64) (int64, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.substrateErr != nil {
		return 0, "", m.substrateErr
	}
	m.nextMachID++
	id := m.nextMachID
	m.machines[id] = &mockMachine{id: id, state: "reported"}
	return id, "reported", nil
}

func (m *mockXoluClient) WalkMachine(ctx context.Context, tenant string, machineID int64, input string, payload map[string]any) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.substrateErr != nil {
		return "", m.substrateErr
	}
	if m.walkErr != nil {
		return "", m.walkErr
	}
	m.walkCalls++
	if payload != nil {
		cp := make(map[string]any, len(payload))
		for k, v := range payload {
			cp[k] = v
		}
		m.lastWalkPayload = cp
	} else {
		m.lastWalkPayload = nil
	}
	mach, ok := m.machines[machineID]
	if !ok {
		return "", taxonomy.NewNotFound(tenant, fmt.Sprintf("machine:%d", machineID))
	}

	// 8-state lifecycle: reported, triaged, in_progress, on_hold, resolved, closed, rejected, duplicate
	switch mach.state {
	case "reported":
		switch input {
		case "triage":
			mach.state = "triaged"
		case "start":
			mach.state = "in_progress"
		case "reject":
			mach.state = "rejected"
		default:
			return "", &taxonomy.TypedError{
				Code:    taxonomy.PrefixXolu + string(taxonomy.CodeInvalidTransition),
				Message: fmt.Sprintf("cannot transition from reported via %s", input),
			}
		}
	case "in_progress":
		switch input {
		case "hold":
			mach.state = "on_hold"
		case "resolve":
			mach.state = "resolved"
		case "reject":
			mach.state = "rejected"
		default:
			return "", &taxonomy.TypedError{
				Code:    taxonomy.PrefixXolu + string(taxonomy.CodeInvalidTransition),
				Message: fmt.Sprintf("cannot transition from in_progress via %s", input),
			}
		}
	case "resolved":
		switch input {
		case "close":
			mach.state = "closed"
		default:
			return "", &taxonomy.TypedError{
				Code:    taxonomy.PrefixXolu + string(taxonomy.CodeInvalidTransition),
				Message: fmt.Sprintf("cannot transition from resolved via %s", input),
			}
		}
	default:
		return "", &taxonomy.TypedError{
			Code:    taxonomy.PrefixXolu + string(taxonomy.CodeInvalidTransition),
			Message: fmt.Sprintf("state %s cannot accept transition %s", mach.state, input),
		}
	}
	return mach.state, nil
}

func TestCreateIssue_Success(t *testing.T) {
	mockX := newMockXolu()
	svc := issues.NewIssueService(mockX)

	req := issues.CreateIssueRequest{
		Tenant:      "tenant-alpha",
		Title:       "Pump oil leakage",
		Description: "Detected continuous dripping",
		Priority:    "high",
		Severity:    "minor",
	}

	issue, err := svc.CreateIssue(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if issue.ID <= 0 {
		t.Errorf("expected positive issue ID, got %d", issue.ID)
	}
	if issue.Title != req.Title {
		t.Errorf("expected title %q, got %q", req.Title, issue.Title)
	}
	if issue.State != "reported" {
		t.Errorf("expected initial state reported, got %q", issue.State)
	}
	if issue.MachineID <= 0 {
		t.Errorf("expected positive machine ID, got %d", issue.MachineID)
	}
	if issue.AssetCategory != "normal" {
		t.Errorf("expected normal asset category, got %q", issue.AssetCategory)
	}
	if issue.SLAFirstResponseDeadline == "" || issue.SLAResolutionDeadline == "" {
		t.Errorf("expected SLA deadlines to be populated")
	}
}

func TestCreateIssue_WithCriticalAsset(t *testing.T) {
	mockX := newMockXolu()
	mockX.entities["assets"] = map[int64]map[string]any{
		200: {"id": int64(200), "name": "Main Turbine", "criticality": "Alta"},
	}

	svc := issues.NewIssueService(mockX)
	req := issues.CreateIssueRequest{
		Tenant:   "tenant-alpha",
		Title:    "Turbine vibration alert",
		Priority: "critical",
		AssetID:  200,
	}

	before := time.Now().UTC()
	issue, err := svc.CreateIssue(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if issue.AssetCategory != "critico" {
		t.Errorf("expected critical asset category, got %q", issue.AssetCategory)
	}

	// Critical priority on critical asset -> 60 min response, 240 min resolution (Seam table)
	respDeadline, err := time.Parse(time.RFC3339, issue.SLAFirstResponseDeadline)
	if err != nil {
		t.Fatalf("failed to parse first response deadline: %v", err)
	}
	diff := respDeadline.Sub(before)
	if diff < 59*time.Minute || diff > 61*time.Minute { 
		t.Errorf("expected ~60 min deadline, got %v", diff)
	}
}

func TestCreateIssue_ValidationErrors(t *testing.T) {
	mockX := newMockXolu()
	svc := issues.NewIssueService(mockX)

	// Missing title
	_, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
		Tenant: "tenant-alpha",
		Title:  "",
	})
	if err == nil {
		t.Fatal("expected error for empty title")
	}
	var te *taxonomy.TypedError
	if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeContractViolation) {
		t.Errorf("expected CONTRACT_VIOLATION, got %v", err)
	}

	// Invalid priority
	_, err = svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
		Tenant:   "tenant-alpha",
		Title:    "Valid Title",
		Priority: "super-urgent",
	})
	if err == nil {
		t.Fatal("expected error for invalid priority")
	}
	if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeContractViolation) {
		t.Errorf("expected CONTRACT_VIOLATION for priority, got %v", err)
	}
}

func TestCreateIssue_AssetNotFound(t *testing.T) {
	mockX := newMockXolu()
	svc := issues.NewIssueService(mockX)

	_, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
		Tenant:  "tenant-alpha",
		Title:   "Issue on missing asset",
		AssetID: 9999,
	})
	if err == nil {
		t.Fatal("expected error for missing asset")
	}
	var te *taxonomy.TypedError
	if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeNotFound) {
		t.Errorf("expected NOT_FOUND for asset, got %v", err)
	}
}

func TestGetIssue_SuccessAndNotFound(t *testing.T) {
	mockX := newMockXolu()
	svc := issues.NewIssueService(mockX)

	created, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
		Tenant: "tenant-alpha",
		Title:  "Issue to get",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := svc.GetIssue(context.Background(), issues.GetIssueRequest{
		Tenant:  "tenant-alpha",
		IssueID: created.ID,
	})
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.ID != created.ID || got.Title != "Issue to get" {
		t.Errorf("retrieved issue mismatch: %+v", got)
	}

	// Not found
	_, err = svc.GetIssue(context.Background(), issues.GetIssueRequest{
		Tenant:  "tenant-alpha",
		IssueID: 8888,
	})
	if err == nil {
		t.Fatal("expected error for non-existent issue")
	}
	var te *taxonomy.TypedError
	if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeNotFound) {
		t.Errorf("expected NOT_FOUND, got %v", err)
	}
}

func TestAdvanceIssue_SuccessAndLifecycle(t *testing.T) {
	mockX := newMockXolu()
	svc := issues.NewIssueService(mockX)

	created, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
		Tenant: "tenant-alpha",
		Title:  "Lifecycle test issue",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	resp, err := svc.AdvanceIssue(context.Background(), issues.AdvanceIssueRequest{
		Tenant:     "tenant-alpha",
		IssueID:    created.ID,
		Transition: "start",
		ActorID:    42,
	})
	if err != nil {
		t.Fatalf("advance failed: %v", err)
	}

	if resp.PreviousState != "reported" || resp.NewState != "in_progress" {
		t.Errorf("expected reported -> in_progress, got %s -> %s", resp.PreviousState, resp.NewState)
	}
	if resp.Issue.FirstResponseAt == "" {
		t.Errorf("expected FirstResponseAt to be recorded on start")
	}
}

func TestAdvanceIssue_InvalidTransition_Verbatim(t *testing.T) {
	mockX := newMockXolu()
	svc := issues.NewIssueService(mockX)

	created, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
		Tenant: "tenant-alpha",
		Title:  "Invalid transition test",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Trying to close directly from reported state
	_, err = svc.AdvanceIssue(context.Background(), issues.AdvanceIssueRequest{
		Tenant:     "tenant-alpha",
		IssueID:    created.ID,
		Transition: "close",
	})
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}

	var te *taxonomy.TypedError
	if !errors.As(err, &te) {
		t.Fatalf("expected TypedError, got %T: %v", err, err)
	}
	if te.Code != taxonomy.PrefixXolu+string(taxonomy.CodeInvalidTransition) {
		t.Errorf("expected XOLU-INVALID_TRANSITION, got %s", te.Code)
	}
	if te.Message != "cannot transition from reported via close" {
		t.Errorf("expected exact verbatim diagnostic message, got %q", te.Message)
	}
}

func TestAdvanceIssue_SubstrateUnavailable(t *testing.T) {
	mockX := newMockXolu()
	mockX.substrateErr = errors.New("connection refused to substrate")
	svc := issues.NewIssueService(mockX)

	_, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
		Tenant: "tenant-alpha",
		Title:  "Failing issue",
	})
	if err == nil {
		t.Fatal("expected error when substrate fails")
	}
	var te *taxonomy.TypedError
	if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeSubstrateUnavailable) {
		t.Errorf("expected SUBSTRATE_UNAVAILABLE, got %v", err)
	}
}

func TestIssueService_Concurrency(t *testing.T) {
	mockX := newMockXolu()
	svc := issues.NewIssueService(mockX)

	var wg sync.WaitGroup
	errCh := make(chan error, 40)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			issue, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
				Tenant:   "tenant-alpha",
				Title:    fmt.Sprintf("Concurrent Issue %d", idx),
				Priority: "medium",
			})
			if err != nil {
				errCh <- err
				return
			}
			_, err = svc.AdvanceIssue(context.Background(), issues.AdvanceIssueRequest{
				Tenant:     "tenant-alpha",
				IssueID:    issue.ID,
				Transition: "start",
				ActorID:    int64(idx + 1),
			})
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}


func TestCreateIssue_AssetCriticalityMapping(t *testing.T) {
	cases := []struct {
		name         string
		assetID      int64
		asset        map[string]any
		wantCategory string
	}{
		{
			name:         "Alta_maps_to_critico",
			assetID:      1,
			asset:        map[string]any{"id": int64(1), "criticality": "Alta"},
			wantCategory: "critico",
		},
		{
			name:         "High_maps_to_critico",
			assetID:      2,
			asset:        map[string]any{"id": int64(2), "criticality": "High"},
			wantCategory: "critico",
		},
		{
			name:         "Media_maps_to_normal",
			assetID:      3,
			asset:        map[string]any{"id": int64(3), "criticality": "Media"},
			wantCategory: "normal",
		},
		{
			name:         "Baja_maps_to_normal",
			assetID:      4,
			asset:        map[string]any{"id": int64(4), "criticality": "Baja"},
			wantCategory: "normal",
		},
		{
			name:         "Medium_maps_to_normal",
			assetID:      5,
			asset:        map[string]any{"id": int64(5), "criticality": "Medium"},
			wantCategory: "normal",
		},
		{
			name:         "Low_maps_to_normal",
			assetID:      6,
			asset:        map[string]any{"id": int64(6), "criticality": "Low"},
			wantCategory: "normal",
		},
		{
			name:         "empty_criticality_maps_to_normal",
			assetID:      7,
			asset:        map[string]any{"id": int64(7), "criticality": ""},
			wantCategory: "normal",
		},
		{
			name:         "unreadable_criticality_maps_to_normal",
			assetID:      8,
			asset:        map[string]any{"id": int64(8), "criticality": 123},
			wantCategory: "normal",
		},
		{
			name:         "old_token_critical_is_normal",
			assetID:      9,
			asset:        map[string]any{"id": int64(9), "criticality": "critical"},
			wantCategory: "normal",
		},
		{
			name:         "absent_asset_maps_to_normal",
			assetID:      0,
			wantCategory: "normal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockX := newMockXolu()
			if tc.asset != nil {
				mockX.entities["assets"] = map[int64]map[string]any{tc.assetID: tc.asset}
			}
			svc := issues.NewIssueService(mockX)
			iss, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
				Tenant:   "dev",
				Title:    "Criticality check",
				Priority: "high",
				AssetID:  tc.assetID,
			})
			if err != nil {
				t.Fatalf("CreateIssue failed: %v", err)
			}
			if iss.AssetCategory != tc.wantCategory {
				t.Fatalf("AssetCategory = %q, want %q", iss.AssetCategory, tc.wantCategory)
			}
			stored, _ := mockX.GetEntity(context.Background(), "dev", "issues", iss.ID)
			if got, _ := stored["asset_category"].(string); got != tc.wantCategory {
				t.Fatalf("stored asset_category = %q, want %q", got, tc.wantCategory)
			}
		})
	}
}

func TestDefaultSLATables_MatchSeam(t *testing.T) {
	cases := []struct {
		category   string
		priority   string
		wantFirst  int64
		wantResolv int64
	}{
		// critico
		{"critico", "critical", 60, 240},
		{"critico", "high", 120, 1440},
		{"critico", "medium", 240, 1440},
		{"critico", "low", 240, 2880},
		{"critico", "unknown", 120, 1440},
		// normal
		{"normal", "critical", 120, 1440},
		{"normal", "high", 240, 2880},
		{"normal", "medium", 480, 4320},
		{"normal", "low", 480, 10080},
		{"normal", "unknown", 240, 2880},
	}

	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.category+"_"+tc.priority, func(t *testing.T) {
			if got := issues.DefaultSLAFirstResponseMinutes(tc.priority, tc.category); got != tc.wantFirst {
				t.Errorf("DefaultSLAFirstResponseMinutes(%q, %q) = %d, want %d", tc.priority, tc.category, got, tc.wantFirst)
			}
			if got := issues.DefaultSLAResolutionMinutes(tc.priority, tc.category); got != tc.wantResolv {
				t.Errorf("DefaultSLAResolutionMinutes(%q, %q) = %d, want %d", tc.priority, tc.category, got, tc.wantResolv)
			}
			gotFirstDL, gotResDL := issues.CalculateSLADeadlines(tc.priority, tc.category, now)
			wantFirstDL := now.Add(time.Duration(tc.wantFirst) * time.Minute).Format(time.RFC3339)
			wantResDL := now.Add(time.Duration(tc.wantResolv) * time.Minute).Format(time.RFC3339)
			if gotFirstDL != wantFirstDL || gotResDL != wantResDL {
				t.Errorf("CalculateSLADeadlines = (%s, %s), want (%s, %s)", gotFirstDL, gotResDL, wantFirstDL, wantResDL)
			}
		})
	}
}

func TestCreateIssue_SeamDocumentFields(t *testing.T) {
	t.Run("with_reporter_and_asset", func(t *testing.T) {
		mockX := newMockXolu()
		mockX.entities["assets"] = map[int64]map[string]any{
			200: {"id": int64(200), "name": "Boiler", "criticality": "Alta"},
		}
		svc := issues.NewIssueService(mockX)

		created, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
			Tenant:      "tenant-alpha",
			Title:       "Pipe leak",
			Description: "Unit 3",
			Priority:    "high",
			Severity:    "major",
			AssetID:     200,
			ReporterID:  77,
		})
		if err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}

		if created.Channel != "api" {
			t.Errorf("created.Channel = %q, want %q", created.Channel, "api")
		}
		if created.ReportedBy != 77 || created.Assignee != 77 {
			t.Errorf("created ReportedBy=%d Assignee=%d, want both 77", created.ReportedBy, created.Assignee)
		}

		stored, err := mockX.GetEntity(context.Background(), "tenant-alpha", "issues", created.ID)
		if err != nil {
			t.Fatalf("GetEntity failed: %v", err)
		}
		if _, hasTenant := stored["tenant"]; hasTenant {
			t.Errorf("stored xolu document must NOT contain tenant key, got %v", stored["tenant"])
		}
		if gotChan, _ := stored["channel"].(string); gotChan != "api" {
			t.Errorf("stored channel = %v, want %q", stored["channel"], "api")
		}
		if stored["reported_by"] != int64(77) || stored["assignee"] != int64(77) {
			t.Errorf("stored reported_by=%v assignee=%v, want 77/77", stored["reported_by"], stored["assignee"])
		}

		wantKeys := map[string]bool{
			"id":                          true,
			"title":                       true,
			"description":                 true,
			"priority":                    true,
			"severity":                    true,
			"state":                       true,
			"machine_id":                  true,
			"channel":                     true,
			"created_at":                  true,
			"asset_id":                    true,
			"asset_category":              true,
			"sla_first_response_deadline": true,
			"sla_resolution_deadline":     true,
			"reported_by":                 true,
			"assignee":                    true,
		}
		if len(stored) != len(wantKeys) {
			t.Errorf("stored keys count = %d, want %d; stored=%v", len(stored), len(wantKeys), stored)
		}
		for k := range stored {
			if !wantKeys[k] {
				t.Errorf("unexpected stored key %q in xolu document", k)
			}
		}

		got, err := svc.GetIssue(context.Background(), issues.GetIssueRequest{
			Tenant:  "tenant-alpha",
			IssueID: created.ID,
		})
		if err != nil {
			t.Fatalf("GetIssue failed: %v", err)
		}
		if got.Channel != "api" || got.ReportedBy != 77 || got.Assignee != 77 {
			t.Errorf("decoded GetIssue channel=%q reported_by=%d assignee=%d, want api/77/77", got.Channel, got.ReportedBy, got.Assignee)
		}

		adv, err := svc.AdvanceIssue(context.Background(), issues.AdvanceIssueRequest{
			Tenant:     "tenant-alpha",
			IssueID:    created.ID,
			Transition: "triage",
			ActorID:    77,
		})
		if err != nil {
			t.Fatalf("AdvanceIssue failed: %v", err)
		}
		if adv.Issue.State != "triaged" || adv.Issue.UpdatedAt == "" || adv.Issue.Channel != "api" {
			t.Errorf("AdvanceIssue result state=%q updated_at=%q channel=%q", adv.Issue.State, adv.Issue.UpdatedAt, adv.Issue.Channel)
		}
		if stored["state"] != "triaged" || stored["updated_at"] == "" || stored["updated_at"] == nil {
			t.Errorf("stored after AdvanceIssue state=%v updated_at=%v", stored["state"], stored["updated_at"])
		}
	})

	t.Run("without_reporter_or_asset_omits_optional_keys", func(t *testing.T) {
		mockX := newMockXolu()
		svc := issues.NewIssueService(mockX)

		created, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
			Tenant: "tenant-alpha",
			Title:  "Anonymous report",
		})
		if err != nil {
			t.Fatalf("CreateIssue failed: %v", err)
		}
		stored, _ := mockX.GetEntity(context.Background(), "tenant-alpha", "issues", created.ID)
		for _, forbidden := range []string{"tenant", "reported_by", "assignee", "asset_id"} {
			if _, ok := stored[forbidden]; ok {
				t.Errorf("stored document should omit %q when unset, got %v", forbidden, stored[forbidden])
			}
		}
		if stored["channel"] != "api" {
			t.Errorf("stored channel = %v, want %q", stored["channel"], "api")
		}
	})
}

func TestAdvanceIssue_RejectWithReason(t *testing.T) {
	t.Run("missing_or_blank_reason_returns_contract_violation", func(t *testing.T) {
		for _, blank := range []string{"", "   \t\n "} {
			mockX := newMockXolu()
			svc := issues.NewIssueService(mockX)
			created, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
				Tenant: "tenant-alpha",
				Title:  "Needs reason to reject",
			})
			if err != nil {
				t.Fatalf("CreateIssue: %v", err)
			}

			_, err = svc.AdvanceIssue(context.Background(), issues.AdvanceIssueRequest{
				Tenant:     "tenant-alpha",
				IssueID:    created.ID,
				Transition: "reject",
				Reason:     blank,
				ActorID:    42,
			})
			var te *taxonomy.TypedError
			if !errors.As(err, &te) || te.Code != taxonomy.FullCode(taxonomy.PrefixMolu, taxonomy.CodeContractViolation) {
				t.Fatalf("expected CONTRACT_VIOLATION for blank reason %q, got %v", blank, err)
			}
			fields, _ := te.Details["fields"].([]string)
			if len(fields) != 1 || fields[0] != "reason" {
				t.Errorf("details.fields = %v, want [reason]", te.Details["fields"])
			}
			if mockX.walkCalls != 0 {
				t.Errorf("WalkMachine called %d times, want 0 when reason missing", mockX.walkCalls)
			}
		}
	})

	t.Run("valid_reason_sends_motivo_actor_and_persists_reject_reason", func(t *testing.T) {
		mockX := newMockXolu()
		svc := issues.NewIssueService(mockX)
		created, err := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
			Tenant: "tenant-alpha",
			Title:  "False alarm",
		})
		if err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}

		resp, err := svc.AdvanceIssue(context.Background(), issues.AdvanceIssueRequest{
			Tenant:     "tenant-alpha",
			IssueID:    created.ID,
			Transition: "reject",
			Reason:     "  Duplicate of sensor test  ",
			ActorID:    42,
		})
		if err != nil {
			t.Fatalf("AdvanceIssue(reject): %v", err)
		}
		if resp.NewState != "rejected" || resp.Issue.State != "rejected" {
			t.Errorf("state = %q / %q, want rejected", resp.NewState, resp.Issue.State)
		}
		if resp.Issue.RejectReason != "Duplicate of sensor test" {
			t.Errorf("RejectReason = %q, want trimmed reason", resp.Issue.RejectReason)
		}
		if gotMotivo, _ := mockX.lastWalkPayload["motivo"].(string); gotMotivo != "Duplicate of sensor test" {
			t.Errorf("walk payload motivo = %v, want %q", mockX.lastWalkPayload["motivo"], "Duplicate of sensor test")
		}
		if gotActor, _ := mockX.lastWalkPayload["actor"].(int64); gotActor != 42 {
			t.Errorf("walk payload actor = %v, want 42", mockX.lastWalkPayload["actor"])
		}

		stored, _ := mockX.GetEntity(context.Background(), "tenant-alpha", "issues", created.ID)
		if stored["reject_reason"] != "Duplicate of sensor test" {
			t.Errorf("stored reject_reason = %v, want trimmed reason", stored["reject_reason"])
		}

		got, err := svc.GetIssue(context.Background(), issues.GetIssueRequest{
			Tenant:  "tenant-alpha",
			IssueID: created.ID,
		})
		if err != nil {
			t.Fatalf("GetIssue: %v", err)
		}
		if got.RejectReason != "Duplicate of sensor test" {
			t.Errorf("GetIssue.RejectReason = %q, want %q", got.RejectReason, "Duplicate of sensor test")
		}
	})

	t.Run("non_reject_transition_ignores_reason_and_sends_actor", func(t *testing.T) {
		mockX := newMockXolu()
		svc := issues.NewIssueService(mockX)
		created, _ := svc.CreateIssue(context.Background(), issues.CreateIssueRequest{
			Tenant: "tenant-alpha",
			Title:  "Valid issue",
		})

		resp, err := svc.AdvanceIssue(context.Background(), issues.AdvanceIssueRequest{
			Tenant:     "tenant-alpha",
			IssueID:    created.ID,
			Transition: "triage",
			Reason:     "ignored note",
			ActorID:    99,
		})
		if err != nil {
			t.Fatalf("AdvanceIssue(triage): %v", err)
		}
		if resp.Issue.RejectReason != "" {
			t.Errorf("non-reject transition stored RejectReason = %q, want empty", resp.Issue.RejectReason)
		}
		if _, hasMotivo := mockX.lastWalkPayload["motivo"]; hasMotivo {
			t.Errorf("non-reject payload must not include motivo, got %v", mockX.lastWalkPayload)
		}
		if gotActor, _ := mockX.lastWalkPayload["actor"].(int64); gotActor != 99 {
			t.Errorf("walk payload actor = %v, want 99", mockX.lastWalkPayload["actor"])
		}
	})
}