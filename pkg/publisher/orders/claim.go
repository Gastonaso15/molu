package orders

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ClaimInput is the payload of work_orders.ClaimOrder. UserID is the person
// who claims. ResponsibleID says on whose behalf: the crew or third party the
// user answers for. It may be left out when exactly one is eligible, and for a
// user order it can only be the user.
type ClaimInput struct {
	OrderID       string `json:"order_id"`
	UserID        ID     `json:"user_id"`
	ResponsibleID ID     `json:"responsible_id,omitempty"`
}

// ClaimResult reports the claim. AlreadyClaimed is true when the same claimant
// had already claimed the order with the same responsible: nothing was written.
type ClaimResult struct {
	Order           WorkOrder `json:"order"`
	ClaimedByUserID string    `json:"claimed_by_user_id"`
	Responsible     Named     `json:"responsible"`
	AlreadyClaimed  bool      `json:"already_claimed,omitempty"`
}

// ClaimOrder takes an order off the job board, as Seam's claim does: only a
// board_posting order nobody has claimed can be claimed, and only by a user who
// is the one the order is for (user), or who answers for a crew or third party
// of the order's responsible type (their responsible_user_id). The write is a
// single update guarded by the order's _version, so of two simultaneous claims
// only one wins. Claiming again with the same user and responsible is a no-op.
func (s *Service) ClaimOrder(ctx context.Context, in ClaimInput) (ClaimResult, error) {
	var problems []fieldProblem
	if strings.TrimSpace(in.OrderID) == "" {
		problems = append(problems, fieldProblem{"order_id", "required"})
	}
	if !positiveID(in.UserID) {
		problems = append(problems, fieldProblem{"user_id", "must be a positive id"})
	}
	problems = append(problems, optionalIDProblem("responsible_id", in.ResponsibleID)...)
	if err := validationFrom(problems); err != nil {
		return ClaimResult{}, err
	}
	userID := strings.TrimSpace(string(in.UserID))

	doc, err := s.sub.GetEntity(ctx, EntityType, in.OrderID)
	if err != nil {
		return ClaimResult{}, classify(err, in.OrderID)
	}
	state, _ := doc["state"].(string)
	if doc["assignment_type"] != AssignmentBoard {
		return ClaimResult{}, rejection("not_claimable", "the order is not on the job board: it was assigned directly", state)
	}
	if current := idString(doc["responsible_id"]); current != "" && current != "0" {
		same := idString(doc["claimed_by_user_id"]) == userID && (in.ResponsibleID == "" || string(in.ResponsibleID) == current)
		if !same {
			return ClaimResult{}, rejection("already_claimed", "the order was already claimed", state)
		}
		name, _ := doc["assigned_to"].(string)
		return ClaimResult{Order: orderFromDoc(in.OrderID, doc), ClaimedByUserID: userID, Responsible: Named{ID: current, Name: name}, AlreadyClaimed: true}, nil
	}

	options, err := s.claimOptions(ctx, doc, userID)
	if err != nil {
		return ClaimResult{}, err
	}
	chosen, err := pickResponsible(options, in.ResponsibleID, doc["responsible_type"])
	if err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			return ClaimResult{}, err
		}
		return ClaimResult{}, rejection("ineligible", err.Error(), state)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]any{
		"assigned_to":        chosen.Name,
		"claimed_by_user_id": numericOrString(userID),
		"responsible_id":     numericOrString(chosen.ID),
		"updated_at":         now,
	}
	version := intOf(doc["_version"])
	if version <= 0 {
		return ClaimResult{}, fmt.Errorf("work order %s has no _version: cannot claim it safely", in.OrderID)
	}
	patch["_version"] = version
	if err := s.sub.PatchEntity(ctx, EntityType, in.OrderID, patch); err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return ClaimResult{}, rejection("already_claimed", "the order was claimed or changed by someone else: read it again", state)
		}
		return ClaimResult{}, classify(err, in.OrderID)
	}
	for k, v := range patch {
		if k != "_version" {
			doc[k] = v
		}
	}
	return ClaimResult{Order: orderFromDoc(in.OrderID, doc), ClaimedByUserID: userID, Responsible: chosen}, nil
}

// claimOptions lists who the user may claim for, by the order's responsible
// type: themselves (user), or the crews / third parties they answer for.
func (s *Service) claimOptions(ctx context.Context, doc map[string]any, userID string) ([]Named, error) {
	var entity string
	switch rt, _ := doc["responsible_type"].(string); rt {
	case "user":
		name := s.userNames(ctx)(userID)
		if name == "" {
			name = "#" + userID
		}
		return []Named{{ID: userID, Name: name}}, nil
	case "crew":
		entity = crewEntity
	case "third_party":
		entity = outsourcedEntity
	default:
		return nil, nil
	}
	rows, err := s.sub.FindEntities(ctx, entity, EntityQuery{
		Where: []Condition{{Field: "responsible_user_id", Op: "=", Value: numericOrString(userID)}},
		Limit: 100, OrderBy: []string{"id"},
	})
	if err != nil {
		if isMissing(err) {
			return nil, nil
		}
		return nil, classify(err, "")
	}
	opts := make([]Named, 0, len(rows))
	for _, r := range rows {
		n := Named{ID: idString(r["id"])}
		n.Name, _ = r["name"].(string)
		opts = append(opts, n)
	}
	sortNamed(opts)
	return opts, nil
}

func sortNamed(n []Named) {
	sort.Slice(n, func(i, j int) bool { return intOf(n[i].ID) < intOf(n[j].ID) })
}

// pickResponsible chooses among the eligible options. An error that is not a
// ValidationError means the user is not eligible.
func pickResponsible(options []Named, want ID, responsibleType any) (Named, error) {
	if len(options) == 0 {
		return Named{}, fmt.Errorf("the user is not responsible for any %s that can claim this order", responsibleTypeLabel(responsibleType))
	}
	if want != "" {
		for _, o := range options {
			if o.ID == string(want) {
				return o, nil
			}
		}
		return Named{}, fmt.Errorf("the user cannot claim this order on behalf of %s %s", responsibleTypeLabel(responsibleType), want)
	}
	if len(options) == 1 {
		return options[0], nil
	}
	var parts []string
	for _, o := range options {
		parts = append(parts, fmt.Sprintf("%s (%s)", o.ID, o.Name))
	}
	return Named{}, validationFrom([]fieldProblem{{"responsible_id", "the user answers for several: " + strings.Join(parts, ", ") + "; choose one"}})
}

func responsibleTypeLabel(rt any) string {
	switch rt {
	case "crew":
		return "crew"
	case "third_party":
		return "third party"
	}
	return "responsible"
}
