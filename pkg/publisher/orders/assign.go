package orders

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// AssignInput is the payload of work_orders.AssignOrder.
//
// AssignmentType defaults to direct_assignment when omitted. For
// direct_assignment, ResponsibleType (user, crew or third_party) and
// ResponsibleID are required; the target record is checked in xolu and its
// name becomes assigned_to. For board_posting, ResponsibleType defines who may
// claim the order and ResponsibleID must be omitted.
type AssignInput struct {
	OrderID         string `json:"order_id"`
	AssignmentType  string `json:"assignment_type,omitempty"`
	ResponsibleType string `json:"responsible_type"`
	ResponsibleID   ID     `json:"responsible_id,omitempty"`
}

// AssignResult reports the assignment applied to an order. Reassigned is true
// when a previous responsible was replaced. AlreadyAssigned is true when the
// order already carried the exact requested assignment (nothing was written).
type AssignResult struct {
	Order           WorkOrder `json:"order"`
	Responsible     *Named    `json:"responsible,omitempty"`
	Reassigned      bool      `json:"reassigned,omitempty"`
	AlreadyAssigned bool      `json:"already_assigned,omitempty"`
}

// AssignOrder assigns or reassigns an open work order to a user, crew or third
// party, or places it on the job board (board_posting). Completed orders are
// rejected with details.rule="final_state". The write is a single PATCH guarded
// by _version, so a concurrent update returns details.rule="stale_state". Any
// previous claim is cleared when assigning or posting back to the board.
func (s *Service) AssignOrder(ctx context.Context, in AssignInput) (AssignResult, error) {
	orderID := strings.TrimSpace(in.OrderID)
	assignType := strings.TrimSpace(in.AssignmentType)
	if assignType == "" {
		assignType = AssignmentDirect
	}
	respType := strings.TrimSpace(in.ResponsibleType)
	rawRespID := strings.TrimSpace(string(in.ResponsibleID))

	var problems []fieldProblem
	if orderID == "" {
		problems = append(problems, fieldProblem{"order_id", "required"})
	}
	switch assignType {
	case AssignmentDirect, AssignmentBoard:
	default:
		problems = append(problems, fieldProblem{"assignment_type", "must be direct_assignment or board_posting"})
	}
	switch respType {
	case "user", "crew", "third_party":
	case "":
		problems = append(problems, fieldProblem{"responsible_type", "required: user, crew or third_party"})
	default:
		problems = append(problems, fieldProblem{"responsible_type", "must be user, crew or third_party"})
	}

	var respID int64
	if assignType == AssignmentBoard {
		if rawRespID != "" {
			problems = append(problems, fieldProblem{"responsible_id", "a board posting has no responsible until someone claims it"})
		}
	} else if assignType == AssignmentDirect {
		if rawRespID == "" {
			problems = append(problems, fieldProblem{"responsible_id", "required for a direct assignment"})
		} else {
			n, err := strconv.ParseInt(rawRespID, 10, 64)
			if err != nil || n <= 0 {
				problems = append(problems, fieldProblem{"responsible_id", "must be a positive id"})
			} else {
				respID = n
			}
		}
	}
	if err := validationFrom(problems); err != nil {
		return AssignResult{}, err
	}

	doc, err := s.sub.GetEntity(ctx, EntityType, orderID)
	if err != nil {
		return AssignResult{}, classify(err, orderID)
	}
	curOrder := orderFromDoc(orderID, doc)
	if curOrder.Terminal {
		return AssignResult{}, rejection("final_state", fmt.Sprintf("the order is already in its final state %q and cannot be assigned", curOrder.State), curOrder.State)
	}

	var targetName string
	var idStr string
	if assignType == AssignmentDirect {
		idStr = strconv.FormatInt(respID, 10)
		targetName, err = s.resolveDirectResponsible(ctx, respType, respID)
		if err != nil {
			return AssignResult{}, err
		}
	}

	curAssignType, _ := doc["assignment_type"].(string)
	curRespType, _ := doc["responsible_type"].(string)
	curRespID := intOf(doc["responsible_id"])
	curClaimed := intOf(doc["claimed_by_user_id"])
	curAssignedTo, _ := doc["assigned_to"].(string)

	if assignType == AssignmentDirect &&
		curAssignType == AssignmentDirect &&
		curRespType == respType &&
		curRespID == respID &&
		curClaimed == 0 {
		name := curAssignedTo
		if strings.TrimSpace(name) == "" {
			name = targetName
		}
		return AssignResult{
			Order:           curOrder,
			Responsible:     &Named{ID: idStr, Name: name},
			AlreadyAssigned: true,
		}, nil
	}
	if assignType == AssignmentBoard &&
		curAssignType == AssignmentBoard &&
		curRespType == respType &&
		curRespID == 0 &&
		curClaimed == 0 {
		return AssignResult{
			Order:           curOrder,
			AlreadyAssigned: true,
		}, nil
	}

	version := intOf(doc["_version"])
	if version <= 0 {
		return AssignResult{}, fmt.Errorf("work order %s has no _version: cannot assign it safely", orderID)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	patch := map[string]any{
		"assignment_type":    assignType,
		"responsible_type":   respType,
		"responsible_id":     respID,
		"assigned_to":        targetName,
		"claimed_by_user_id": int64(0),
		"updated_at":         now,
		"_version":           version,
	}
	if err := s.sub.PatchEntity(ctx, EntityType, orderID, patch); err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return AssignResult{}, rejection("stale_state", "the order changed while it was being assigned: read it again and retry", curOrder.State)
		}
		return AssignResult{}, classify(err, orderID)
	}
	for k, v := range patch {
		if k != "_version" {
			doc[k] = v
		}
	}

	reassigned := curRespID > 0 || (curAssignType == "" && strings.TrimSpace(curAssignedTo) != "")
	var resp *Named
	if assignType == AssignmentDirect {
		resp = &Named{ID: idStr, Name: targetName}
	}
	return AssignResult{
		Order:       orderFromDoc(orderID, doc),
		Responsible: resp,
		Reassigned:  reassigned,
	}, nil
}

// resolveDirectResponsible checks that the target user, crew or third party
// exists in xolu and returns its display name. For users, only name and active
// status are read (never credentials or email).
func (s *Service) resolveDirectResponsible(ctx context.Context, respType string, respID int64) (string, error) {
	var entity string
	switch respType {
	case "user":
		entity = usersEntity
	case "crew":
		entity = crewEntity
	case "third_party":
		entity = outsourcedEntity
	default:
		return "", validationFrom([]fieldProblem{{"responsible_type", "must be user, crew or third_party"}})
	}
	idStr := strconv.FormatInt(respID, 10)
	doc, err := s.sub.GetEntity(ctx, entity, idStr)
	if err != nil {
		if isMissing(err) {
			return "", validationFrom([]fieldProblem{{"responsible_id", fmt.Sprintf("%s %d does not exist", respType, respID)}})
		}
		return "", classify(err, "")
	}
	if respType == "user" {
		if active, hasActive := doc["active"].(bool); hasActive && !active {
			return "", validationFrom([]fieldProblem{{"responsible_id", fmt.Sprintf("user %d is inactive", respID)}})
		}
	}
	name, _ := doc["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return "", validationFrom([]fieldProblem{{"responsible_id", fmt.Sprintf("%s %d has no name", respType, respID)}})
	}
	return name, nil
}
