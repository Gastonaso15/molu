package issues

import (
	"context"
	"time"
)

const (
	// Namespace prefixes every function published by this service.
	Namespace = "issues"
	// PublisherID identifies this publisher in the Hub catalog.
	PublisherID = "issue-tracker-publisher"
	// EntityType is the xolu entity that stores issues.
	EntityType = "issues"
	// DefinitionName is the xolu FSM definition that governs the issue lifecycle.
	DefinitionName = "issue_lifecycle"
	// TokenRef is the reference the Execution Bridge resolves to the bearer token.
	TokenRef = "issues:api-token"
)

// Issue represents an issue tracked by the domain service.
type Issue struct {
	ID                       int64          `json:"id"`
	Tenant                   string         `json:"tenant"`
	Title                    string         `json:"title"`
	Description              string         `json:"description,omitempty"`
	Priority                 string         `json:"priority"`
	Severity                 string         `json:"severity"`
	State                    string         `json:"state"`
	MachineID                int64          `json:"machine_id"`
	AssetID                  int64          `json:"asset_id,omitempty"`
	AssetCategory            string         `json:"asset_category"`
	SLAFirstResponseDeadline string         `json:"sla_first_response_deadline,omitempty"`
	SLAResolutionDeadline    string         `json:"sla_resolution_deadline,omitempty"`
	FirstResponseAt          string         `json:"first_response_at,omitempty"`
	FirstRespondedBy         int64          `json:"first_responded_by,omitempty"`
	CreatedAt                string         `json:"created_at"`
	UpdatedAt                string         `json:"updated_at,omitempty"`
	ReportedBy               int64          `json:"reported_by,omitempty"`
	Assignee                 int64          `json:"assignee,omitempty"`
	Metadata                 map[string]any `json:"metadata,omitempty"`
}

// CreateIssueRequest holds inputs for creating a new issue.
type CreateIssueRequest struct {
	Tenant      string `json:"tenant"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Severity    string `json:"severity,omitempty"`
	AssetID     int64  `json:"asset_id,omitempty"`
	ReporterID  int64  `json:"reporter_id,omitempty"`
}

// AdvanceIssueRequest holds inputs for advancing an issue's lifecycle.
type AdvanceIssueRequest struct {
	Tenant     string `json:"tenant"`
	IssueID    int64  `json:"issue_id"`
	Transition string `json:"transition"`
	ActorID    int64  `json:"actor_id,omitempty"`
}

// AdvanceIssueResponse is returned upon a successful lifecycle transition.
type AdvanceIssueResponse struct {
	Issue         Issue  `json:"issue"`
	PreviousState string `json:"previous_state"`
	NewState      string `json:"new_state"`
}

// GetIssueRequest holds parameters for retrieving an issue.
type GetIssueRequest struct {
	Tenant  string `json:"tenant"`
	IssueID int64  `json:"issue_id"`
}

// XoluClient defines the substrate operations required by the Issue Tracker service.
type XoluClient interface {
	GetEntity(ctx context.Context, tenant, entityType string, id int64) (map[string]any, error)
	CreateEntity(ctx context.Context, tenant, entityType string, data map[string]any) (int64, error)
	PatchEntity(ctx context.Context, tenant, entityType string, id int64, patch map[string]any) error
	EnsureMachineDefinition(ctx context.Context, tenant string) (int64, error)
	CreateMachine(ctx context.Context, tenant string, defID int64) (machineID int64, initialState string, err error)
	WalkMachine(ctx context.Context, tenant string, machineID int64, input string, payload map[string]any) (nextState string, err error)
}

// Default SLA calculation helpers based on priority and asset criticality.
func DefaultSLAFirstResponseMinutes(priority, assetCategory string) int64 {
	switch priority {
	case "critical":
		if assetCategory == "critical" {
			return 15
		}
		return 30
	case "high":
		if assetCategory == "critical" {
			return 60
		}
		return 120
	case "medium":
		if assetCategory == "critical" {
			return 240
		}
		return 480
	case "low":
		return 1440
	default:
		return 480
	}
}

func DefaultSLAResolutionMinutes(priority, assetCategory string) int64 {
	switch priority {
	case "critical":
		if assetCategory == "critical" {
			return 120
		}
		return 240
	case "high":
		if assetCategory == "critical" {
			return 480
		}
		return 960
	case "medium":
		if assetCategory == "critical" {
			return 1440
		}
		return 2880
	case "low":
		return 4320
	default:
		return 2880
	}
}

func CalculateSLADeadlines(priority, assetCategory string, now time.Time) (string, string) {
	respMin := DefaultSLAFirstResponseMinutes(priority, assetCategory)
	resMin := DefaultSLAResolutionMinutes(priority, assetCategory)

	firstRespDeadline := now.Add(time.Duration(respMin) * time.Minute).Format(time.RFC3339)
	resDeadline := now.Add(time.Duration(resMin) * time.Minute).Format(time.RFC3339)

	return firstRespDeadline, resDeadline
}

