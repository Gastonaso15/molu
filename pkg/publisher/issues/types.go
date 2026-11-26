package issues

import (
	"context"
	"strings"
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
	DefinitionName = "seam_issue_lifecycle"
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

const (
	// AssetCategoryCritico marks issues linked to high-criticality assets.
	AssetCategoryCritico = "critico"
	// AssetCategoryNormal marks issues linked to medium/low or absent assets.
	AssetCategoryNormal = "normal"
)

// AssetCategoryFromCriticality maps an asset criticality token ("Alta"/"High" -> "critico",
// "Media"/"Baja"/"Medium"/"Low" or unknown/empty -> "normal") matching Seam rules.
func AssetCategoryFromCriticality(criticality string) string {
	switch strings.ToLower(strings.TrimSpace(criticality)) {
	case "alta", "high":
		return AssetCategoryCritico
	default:
		return AssetCategoryNormal
	}
}

type slaTable struct {
	critico    map[string]int64
	criticoDef int64
	normal     map[string]int64
	normalDef  int64
}

func (t slaTable) lookup(priority, assetCategory string) int64 {
	if assetCategory == AssetCategoryCritico {
		if m, ok := t.critico[priority]; ok {
			return m
		}
		return t.criticoDef
	}
	if m, ok := t.normal[priority]; ok {
		return m
	}
	return t.normalDef
}

var (
	firstResponseSLA = slaTable{
		critico:    map[string]int64{"critical": 60, "high": 120, "medium": 240, "low": 240},
		criticoDef: 120,
		normal:     map[string]int64{"critical": 120, "high": 240, "medium": 480, "low": 480},
		normalDef:  240,
	}
	resolutionSLA = slaTable{
		critico:    map[string]int64{"critical": 240, "high": 1440, "medium": 1440, "low": 2880},
		criticoDef: 1440,
		normal:     map[string]int64{"critical": 1440, "high": 2880, "medium": 4320, "low": 10080},
		normalDef:  2880,
	}
)

// DefaultSLAFirstResponseMinutes returns Seam default first-response minutes by priority and asset category.
// Note: Seam allows per-template overrides (slaMinutesFromTemplate); this publisher uses only these defaults.
func DefaultSLAFirstResponseMinutes(priority, assetCategory string) int64 {
	return firstResponseSLA.lookup(priority, assetCategory)
}

// DefaultSLAResolutionMinutes returns Seam default resolution minutes by priority and asset category.
// Note: Seam allows per-template overrides (slaMinutesFromTemplate); this publisher uses only these defaults.
func DefaultSLAResolutionMinutes(priority, assetCategory string) int64 {
	return resolutionSLA.lookup(priority, assetCategory)
}

func CalculateSLADeadlines(priority, assetCategory string, now time.Time) (string, string) {
	respMin := DefaultSLAFirstResponseMinutes(priority, assetCategory)
	resMin := DefaultSLAResolutionMinutes(priority, assetCategory)

	firstRespDeadline := now.Add(time.Duration(respMin) * time.Minute).Format(time.RFC3339)
	resDeadline := now.Add(time.Duration(resMin) * time.Minute).Format(time.RFC3339)

	return firstRespDeadline, resDeadline
}
