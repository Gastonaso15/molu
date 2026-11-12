package issues

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// IssueService implements domain logic and substrate coordination for issues.
type IssueService struct {
	xolu XoluClient
}

// NewIssueService creates a new IssueService with the given substrate client.
func NewIssueService(xolu XoluClient) *IssueService {
	return &IssueService{
		xolu: xolu,
	}
}

// CreateIssue validates inputs, resolves asset category, computes SLA deadlines,
// instantiates a lifecycle FSM in xolu, and persists the issue entity.
func (s *IssueService) CreateIssue(ctx context.Context, req CreateIssueRequest) (Issue, error) {
	var invalidFields []string

	if strings.TrimSpace(req.Title) == "" {
		invalidFields = append(invalidFields, "title")
	}

	if req.Priority == "" {
		req.Priority = "medium"
	} else {
		switch req.Priority {
		case "critical", "high", "medium", "low":
		default:
			invalidFields = append(invalidFields, "priority")
		}
	}

	if req.Severity == "" {
		req.Severity = "minor"
	} else {
		switch req.Severity {
		case "critical", "major", "minor":
		default:
			invalidFields = append(invalidFields, "severity")
		}
	}

	if len(invalidFields) > 0 {
		return Issue{}, taxonomy.NewContractViolation(req.Tenant, "issues.CreateIssue", invalidFields)
	}

	now := time.Now().UTC()
	assetCategory := "normal"

	if req.AssetID > 0 {
		assetEntity, err := s.xolu.GetEntity(ctx, req.Tenant, "assets", req.AssetID)
		if err != nil {
			return Issue{}, s.mapSubstrateError(req.Tenant, "assets", fmt.Sprintf("%d", req.AssetID), err)
		}
		if crit, ok := assetEntity["criticality"].(string); ok && crit == "critical" {
			assetCategory = "critical"
		}
	}

	firstRespDeadline, resDeadline := CalculateSLADeadlines(req.Priority, assetCategory, now)

	defID, err := s.xolu.EnsureMachineDefinition(ctx, req.Tenant)
	if err != nil {
		return Issue{}, s.mapSubstrateError(req.Tenant, "machine_def", "issue_lifecycle", err)
	}

	machID, initialState, err := s.xolu.CreateMachine(ctx, req.Tenant, defID)
	if err != nil {
		return Issue{}, s.mapSubstrateError(req.Tenant, "machine", "create", err)
	}

	createdAtStr := now.Format(time.RFC3339)
	data := map[string]any{
		"tenant":                      req.Tenant,
		"title":                       req.Title,
		"description":                 req.Description,
		"priority":                    req.Priority,
		"severity":                    req.Severity,
		"state":                       initialState,
		"machine_id":                  machID,
		"asset_category":              assetCategory,
		"sla_first_response_deadline": firstRespDeadline,
		"sla_resolution_deadline":     resDeadline,
		"created_at":                  createdAtStr,
	}

	if req.AssetID > 0 {
		data["asset_id"] = req.AssetID
	}
	if req.ReporterID > 0 {
		data["reported_by"] = req.ReporterID
	}

	entityID, err := s.xolu.CreateEntity(ctx, req.Tenant, "issues", data)
	if err != nil {
		return Issue{}, s.mapSubstrateError(req.Tenant, "issues", "create", err)
	}

	return Issue{
		ID:                       entityID,
		Tenant:                   req.Tenant,
		Title:                    req.Title,
		Description:              req.Description,
		Priority:                 req.Priority,
		Severity:                 req.Severity,
		State:                    initialState,
		MachineID:                machID,
		AssetID:                  req.AssetID,
		AssetCategory:            assetCategory,
		SLAFirstResponseDeadline: firstRespDeadline,
		SLAResolutionDeadline:    resDeadline,
		CreatedAt:                createdAtStr,
		ReportedBy:               req.ReporterID,
	}, nil
}

// GetIssue retrieves an issue from xolu and decodes it.
func (s *IssueService) GetIssue(ctx context.Context, req GetIssueRequest) (Issue, error) {
	data, err := s.xolu.GetEntity(ctx, req.Tenant, "issues", req.IssueID)
	if err != nil {
		return Issue{}, s.mapSubstrateError(req.Tenant, "issues", fmt.Sprintf("%d", req.IssueID), err)
	}

	return decodeIssue(req.Tenant, req.IssueID, data), nil
}

// AdvanceIssue performs a lifecycle transition on the issue's FSM in xolu.
func (s *IssueService) AdvanceIssue(ctx context.Context, req AdvanceIssueRequest) (AdvanceIssueResponse, error) {
	data, err := s.xolu.GetEntity(ctx, req.Tenant, "issues", req.IssueID)
	if err != nil {
		return AdvanceIssueResponse{}, s.mapSubstrateError(req.Tenant, "issues", fmt.Sprintf("%d", req.IssueID), err)
	}

	issue := decodeIssue(req.Tenant, req.IssueID, data)
	previousState := issue.State

	now := time.Now().UTC()
	patch := make(map[string]any)

	// If transitioning to start (in_progress), mark first response time if unset
	if req.Transition == "start" && issue.FirstResponseAt == "" {
		firstRespTime := now.Format(time.RFC3339)
		patch["first_response_at"] = firstRespTime
		patch["first_responded_by"] = req.ActorID
		issue.FirstResponseAt = firstRespTime
		issue.FirstRespondedBy = req.ActorID
	}

	payload := map[string]any{}
	if req.ActorID > 0 {
		payload["actor"] = req.ActorID
	}

	nextState, err := s.xolu.WalkMachine(ctx, req.Tenant, issue.MachineID, req.Transition, payload)
	if err != nil {
		var te *taxonomy.TypedError
		if errors.As(err, &te) {
			return AdvanceIssueResponse{}, te
		}
		return AdvanceIssueResponse{}, s.mapSubstrateError(req.Tenant, "machine", fmt.Sprintf("%d", issue.MachineID), err)
	}

	patch["state"] = nextState
	patch["updated_at"] = now.Format(time.RFC3339)

	if err := s.xolu.PatchEntity(ctx, req.Tenant, "issues", req.IssueID, patch); err != nil {
		return AdvanceIssueResponse{}, s.mapSubstrateError(req.Tenant, "issues", fmt.Sprintf("%d", req.IssueID), err)
	}

	issue.State = nextState
	issue.UpdatedAt = patch["updated_at"].(string)

	return AdvanceIssueResponse{
		Issue:         issue,
		PreviousState: previousState,
		NewState:      nextState,
	}, nil
}

func (s *IssueService) mapSubstrateError(tenant, entityType, id string, err error) error {
	var te *taxonomy.TypedError
	if errors.As(err, &te) {
		return te
	}
	now := taxonomy.NowRFC3339()
	nextRetry := taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 5*time.Second)
	return taxonomy.NewSubstrateUnavailable(now, nextRetry, 1, 3)
}

func decodeIssue(tenant string, id int64, data map[string]any) Issue {
	issue := Issue{
		ID:     id,
		Tenant: tenant,
	}
	if v, ok := data["title"].(string); ok {
		issue.Title = v
	}
	if v, ok := data["description"].(string); ok {
		issue.Description = v
	}
	if v, ok := data["priority"].(string); ok {
		issue.Priority = v
	}
	if v, ok := data["severity"].(string); ok {
		issue.Severity = v
	}
	if v, ok := data["state"].(string); ok {
		issue.State = v
	}
	if v, ok := data["machine_id"]; ok {
		issue.MachineID = toInt64(v)
	}
	if v, ok := data["asset_id"]; ok {
		issue.AssetID = toInt64(v)
	}
	if v, ok := data["asset_category"].(string); ok {
		issue.AssetCategory = v
	}
	if v, ok := data["sla_first_response_deadline"].(string); ok {
		issue.SLAFirstResponseDeadline = v
	}
	if v, ok := data["sla_resolution_deadline"].(string); ok {
		issue.SLAResolutionDeadline = v
	}
	if v, ok := data["first_response_at"].(string); ok {
		issue.FirstResponseAt = v
	}
	if v, ok := data["first_responded_by"]; ok {
		issue.FirstRespondedBy = toInt64(v)
	}
	if v, ok := data["created_at"].(string); ok {
		issue.CreatedAt = v
	}
	if v, ok := data["updated_at"].(string); ok {
		issue.UpdatedAt = v
	}
	if v, ok := data["reported_by"]; ok {
		issue.ReportedBy = toInt64(v)
	}
	if v, ok := data["assignee"]; ok {
		issue.Assignee = toInt64(v)
	}
	return issue
}

func toInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	default:
		return 0
	}
}

