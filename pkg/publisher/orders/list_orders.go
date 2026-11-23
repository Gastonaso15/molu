package orders

import (
	"context"
	"time"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
	// assetScanLimit bounds the rows read when asset_id is given: assets live
	// in an array inside each document, which OQL cannot filter.
	assetScanLimit = 500
)

// ListOrdersInput is the payload of work_orders.ListOrders. Every filter is
// optional and they combine with AND.
type ListOrdersInput struct {
	State           string `json:"state,omitempty"`
	TemplateID      ID     `json:"template_id,omitempty"`
	AssignmentType  string `json:"assignment_type,omitempty"`
	ResponsibleType string `json:"responsible_type,omitempty"`
	ResponsibleID   ID     `json:"responsible_id,omitempty"`
	Backlog         *bool  `json:"backlog,omitempty"`
	AssetID         ID     `json:"asset_id,omitempty"`
	CreatedFrom     string `json:"created_from,omitempty"`
	CreatedTo       string `json:"created_to,omitempty"`
	Limit           int    `json:"limit,omitempty"`
}

// OrderSummary is one row of ListOrders: the order plus what a job-board or
// dashboard view needs.
type OrderSummary struct {
	WorkOrder
	CreatedAt string `json:"created_at,omitempty"`
	// AwaitingClaim is true for a board_posting order nobody has claimed.
	AwaitingClaim bool `json:"awaiting_claim,omitempty"`
}

// OrderList is the result of work_orders.ListOrders. Total counts the orders
// returned; Truncated says more orders matched than were returned.
type OrderList struct {
	Orders    []OrderSummary `json:"orders"`
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated,omitempty"`
}

var responsibleTypes = map[string]bool{"user": true, "crew": true, "third_party": true}

// ListOrders reads the work_orders entity with the requested filters. An
// entity that does not exist yet (xolu answers XOLU-QL004 until the first
// order is created) is an empty list, as in Seam's own list page.
func (s *Service) ListOrders(ctx context.Context, in ListOrdersInput) (OrderList, error) {
	var problems []fieldProblem
	limit := in.Limit
	switch {
	case limit < 0 || limit > maxListLimit:
		problems = append(problems, fieldProblem{"limit", "must be between 1 and 200"})
	case limit == 0:
		limit = defaultListLimit
	}
	if in.ResponsibleType != "" && !responsibleTypes[in.ResponsibleType] {
		problems = append(problems, fieldProblem{"responsible_type", "must be user, crew or third_party"})
	}
	if in.ResponsibleID != "" && in.ResponsibleType == "" {
		problems = append(problems, fieldProblem{"responsible_id", "needs responsible_type"})
	}
	if in.AssignmentType != "" && in.AssignmentType != AssignmentDirect && in.AssignmentType != AssignmentBoard {
		problems = append(problems, fieldProblem{"assignment_type", "must be direct_assignment or board_posting"})
	}
	backlog := in.Backlog != nil && *in.Backlog
	if backlog && in.AssignmentType != "" && in.AssignmentType != AssignmentBoard {
		problems = append(problems, fieldProblem{"backlog", "only board_posting orders can be in the backlog"})
	}
	from, fromErr := parseBound(in.CreatedFrom, false)
	if fromErr != nil {
		problems = append(problems, fieldProblem{"created_from", "must be an RFC 3339 timestamp or YYYY-MM-DD"})
	}
	to, toErr := parseBound(in.CreatedTo, true)
	if toErr != nil {
		problems = append(problems, fieldProblem{"created_to", "must be an RFC 3339 timestamp or YYYY-MM-DD"})
	}
	if err := validationFrom(problems); err != nil {
		return OrderList{}, err
	}

	var where []Condition
	add := func(field, op string, v any) { where = append(where, Condition{field, op, v}) }
	if in.State != "" {
		add("state", "=", in.State)
	}
	if in.TemplateID != "" {
		add("template_id", "=", idValue(in.TemplateID))
	}
	switch {
	case backlog:
		add("assignment_type", "=", AssignmentBoard)
		add("responsible_id", "=", int64(0))
	case in.AssignmentType != "":
		add("assignment_type", "=", in.AssignmentType)
	}
	if in.ResponsibleType != "" {
		add("responsible_type", "=", in.ResponsibleType)
	}
	if in.ResponsibleID != "" {
		add("responsible_id", "=", idValue(in.ResponsibleID))
	}
	if from != "" {
		add("created_at", ">=", from)
	}
	if to != "" {
		add("created_at", "<=", to)
	}

	fetch := limit + 1 // one extra row tells whether the list was cut
	if in.AssetID != "" {
		fetch = assetScanLimit
	}
	rows, err := s.sub.FindEntities(ctx, EntityType, EntityQuery{Where: where, Limit: fetch})
	if err != nil {
		if isMissing(err) {
			return OrderList{Orders: []OrderSummary{}}, nil
		}
		return OrderList{}, classify(err, "")
	}
	out := OrderList{Orders: []OrderSummary{}}
	if in.AssetID != "" && len(rows) >= assetScanLimit {
		out.Truncated = true // older orders were not scanned
	}
	for _, row := range rows {
		if in.AssetID != "" && !hasID(idList(row["asset_ids"]), string(in.AssetID)) {
			continue
		}
		if len(out.Orders) == limit {
			out.Truncated = true
			break
		}
		id := idString(row["id"])
		sum := OrderSummary{WorkOrder: orderFromDoc(id, row)}
		sum.CreatedAt, _ = row["created_at"].(string)
		sum.AwaitingClaim = row["assignment_type"] == AssignmentBoard && sum.ResponsibleID == ""
		out.Orders = append(out.Orders, sum)
	}
	out.Total = len(out.Orders)
	return out, nil
}

// idValue sends numeric ids as numbers, which is how xolu stores them.
func idValue(id ID) any { return numericOrString(string(id)) }

func hasID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// parseBound turns a date or timestamp into the UTC RFC 3339 text the
// created_at column holds. A bare date means the start of that day, or the end
// of it when endOfDay is set.
func parseBound(s string, endOfDay bool) (string, error) {
	if s == "" {
		return "", nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return "", err
	}
	if endOfDay {
		t = t.Add(24*time.Hour - time.Second)
	}
	return t.UTC().Format(time.RFC3339), nil
}
