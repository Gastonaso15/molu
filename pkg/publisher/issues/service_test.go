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
		200: {"id": int64(200), "name": "Main Turbine", "criticality": "critical"},
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

	if issue.AssetCategory != "critical" {
		t.Errorf("expected critical asset category, got %q", issue.AssetCategory)
	}

	// Critical priority on critical asset -> 15 min response, 120 min resolution
	respDeadline, err := time.Parse(time.RFC3339, issue.SLAFirstResponseDeadline)
	if err != nil {
		t.Fatalf("failed to parse first response deadline: %v", err)
	}
	diff := respDeadline.Sub(before)
	if diff < 14*time.Minute || diff > 16*time.Minute {
		t.Errorf("expected ~15 min deadline, got %v", diff)
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
