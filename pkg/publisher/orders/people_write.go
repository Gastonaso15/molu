package orders

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// CreateCrewInput is the payload of work_orders.CreateCrew.
// A crew needs a non-empty name, a responsible user and at least two distinct
// members. If ResponsibleUserID is not among MemberUserIDs it is added
// automatically.
type CreateCrewInput struct {
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	ResponsibleUserID ID     `json:"responsible_user_id"`
	MemberUserIDs     []ID   `json:"member_user_ids"`
}

// UpdateCrewInput is the payload of work_orders.UpdateCrew. Only provided
// fields change; at least one updatable field must be sent.
type UpdateCrewInput struct {
	CrewID            ID      `json:"crew_id"`
	Name              *string `json:"name,omitempty"`
	Description       *string `json:"description,omitempty"`
	ResponsibleUserID *ID     `json:"responsible_user_id,omitempty"`
	MemberUserIDs     *[]ID   `json:"member_user_ids,omitempty"`
}

// CreateOutsourcedInput is the payload of work_orders.CreateOutsourced.
type CreateOutsourcedInput struct {
	Name                   string `json:"name"`
	Description            string `json:"description,omitempty"`
	ResponsibleUserID      ID     `json:"responsible_user_id,omitempty"`
	Signature              string `json:"signature,omitempty"`
	SignatureClarification string `json:"signature_clarification,omitempty"`
}

// UpdateOutsourcedInput is the payload of work_orders.UpdateOutsourced. Only
// provided fields change; at least one updatable field must be sent. Setting
// ResponsibleUserID to "" or "0" unlinks the representative account.
type UpdateOutsourcedInput struct {
	OutsourcedID           ID      `json:"outsourced_id"`
	Name                   *string `json:"name,omitempty"`
	Description            *string `json:"description,omitempty"`
	ResponsibleUserID      *ID     `json:"responsible_user_id,omitempty"`
	Signature              *string `json:"signature,omitempty"`
	SignatureClarification *string `json:"signature_clarification,omitempty"`
}

// CreateCrew creates an internal team in the crews entity, validating that the
// responsible user and at least two distinct members exist and are active.
func (s *Service) CreateCrew(ctx context.Context, in CreateCrewInput) (Crew, error) {
	name := strings.TrimSpace(in.Name)
	var problems []fieldProblem
	if name == "" {
		problems = append(problems, fieldProblem{"name", "required"})
	}
	respID, ok := parsePositiveID(in.ResponsibleUserID)
	if !ok {
		problems = append(problems, fieldProblem{"responsible_user_id", "must be a positive id"})
	}
	members, memProblems := parseDistinctPositiveIDs("member_user_ids", in.MemberUserIDs, 2)
	problems = append(problems, memProblems...)
	if err := validationFrom(problems); err != nil {
		return Crew{}, err
	}

	names, userProblems, err := s.validateActiveUsers(ctx, map[string][]int64{
		"responsible_user_id": {respID},
		"member_user_ids":     members,
	})
	if err != nil {
		return Crew{}, err
	}
	if err := validationFrom(userProblems); err != nil {
		return Crew{}, err
	}

	members = ensureMemberIncluded(members, respID)
	doc := map[string]any{
		"name":                name,
		"description":         in.Description,
		"responsible_user_id": respID,
		"members":             members,
	}
	id, err := s.sub.CreateEntity(ctx, crewEntity, doc)
	if err != nil {
		return Crew{}, classify(err, "")
	}
	return crewWithNames(id, doc, names), nil
}

// UpdateCrew updates an existing crew (partial PATCH guarded by _version when
// present). When responsible_user_id changes, the new responsible is ensured
// to be present in members.
func (s *Service) UpdateCrew(ctx context.Context, in UpdateCrewInput) (Crew, error) {
	if !positiveID(in.CrewID) {
		return Crew{}, &ValidationError{Fields: []string{"crew_id"}, Reason: "must be a positive id"}
	}
	if in.Name == nil && in.Description == nil && in.ResponsibleUserID == nil && in.MemberUserIDs == nil {
		return Crew{}, validationFrom([]fieldProblem{{"crew_id", "provide at least one field to update"}})
	}

	var problems []fieldProblem
	var newName string
	if in.Name != nil {
		newName = strings.TrimSpace(*in.Name)
		if newName == "" {
			problems = append(problems, fieldProblem{"name", "must not be empty"})
		}
	}
	var newRespID int64
	if in.ResponsibleUserID != nil {
		id, ok := parsePositiveID(*in.ResponsibleUserID)
		if !ok {
			problems = append(problems, fieldProblem{"responsible_user_id", "must be a positive id"})
		} else {
			newRespID = id
		}
	}
	var newMembers []int64
	if in.MemberUserIDs != nil {
		m, p := parseDistinctPositiveIDs("member_user_ids", *in.MemberUserIDs, 2)
		newMembers = m
		problems = append(problems, p...)
	}
	if err := validationFrom(problems); err != nil {
		return Crew{}, err
	}

	id := strings.TrimSpace(string(in.CrewID))
	doc, err := s.sub.GetEntity(ctx, crewEntity, id)
	if err != nil {
		if isMissing(err) {
			return Crew{}, &NotFoundError{ID: id, Kind: "crew"}
		}
		return Crew{}, classify(err, "")
	}

	toCheck := map[string][]int64{}
	if newRespID > 0 {
		toCheck["responsible_user_id"] = []int64{newRespID}
	}
	if in.MemberUserIDs != nil {
		toCheck["member_user_ids"] = newMembers
	}
	names, userProblems, err := s.validateActiveUsers(ctx, toCheck)
	if err != nil {
		return Crew{}, err
	}
	if err := validationFrom(userProblems); err != nil {
		return Crew{}, err
	}

	patch := map[string]any{}
	if in.Name != nil {
		patch["name"] = newName
	}
	if in.Description != nil {
		patch["description"] = *in.Description
	}
	effectiveResp := intOf(doc["responsible_user_id"])
	if newRespID > 0 {
		patch["responsible_user_id"] = newRespID
		effectiveResp = newRespID
	}
	if in.MemberUserIDs != nil {
		effectiveMembers := ensureMemberIncluded(newMembers, effectiveResp)
		patch["members"] = effectiveMembers
	} else if newRespID > 0 {
		existing := int64IDList(doc["members"])
		if !containsInt64(existing, newRespID) {
			patch["members"] = append(existing, newRespID)
		}
	}
	if v := intOf(doc["_version"]); v > 0 {
		patch["_version"] = v
	}

	if err := s.sub.PatchEntity(ctx, crewEntity, id, patch); err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return Crew{}, rejection("stale_state", "the crew changed while it was being updated: read it again and retry", "")
		}
		return Crew{}, classify(err, "")
	}
	for k, v := range patch {
		if k != "_version" {
			doc[k] = v
		}
	}
	lookup := s.userNames(ctx)
	c := crewFromDoc(id, doc)
	if c.Responsible != nil {
		if n, ok := names[c.Responsible.UserID]; ok {
			c.Responsible.Name = n
		} else {
			c.Responsible.Name = lookup(c.Responsible.UserID)
		}
	}
	for i := range c.Members {
		if n, ok := names[c.Members[i].UserID]; ok {
			c.Members[i].Name = n
		} else {
			c.Members[i].Name = lookup(c.Members[i].UserID)
		}
	}
	return c, nil
}

// CreateOutsourced creates a third-party provider record in the outsourced
// entity. The signature image (if supplied) is stored but never returned;
// HasSignature reports whether one is present.
func (s *Service) CreateOutsourced(ctx context.Context, in CreateOutsourcedInput) (Outsourced, error) {
	name := strings.TrimSpace(in.Name)
	var problems []fieldProblem
	if name == "" {
		problems = append(problems, fieldProblem{"name", "required"})
	}
	var respID int64
	if strings.TrimSpace(string(in.ResponsibleUserID)) != "" {
		id, ok := parsePositiveID(in.ResponsibleUserID)
		if !ok {
			problems = append(problems, fieldProblem{"responsible_user_id", "must be a positive id"})
		} else {
			respID = id
		}
	}
	if err := validationFrom(problems); err != nil {
		return Outsourced{}, err
	}

	var respName string
	if respID > 0 {
		names, userProblems, err := s.validateActiveUsers(ctx, map[string][]int64{"responsible_user_id": {respID}})
		if err != nil {
			return Outsourced{}, err
		}
		if err := validationFrom(userProblems); err != nil {
			return Outsourced{}, err
		}
		respName = names[strconv.FormatInt(respID, 10)]
	}

	doc := map[string]any{
		"name":                    name,
		"description":             in.Description,
		"signature":               in.Signature,
		"signature_clarification": in.SignatureClarification,
	}
	if respID > 0 {
		doc["responsible_user_id"] = respID
	}
	id, err := s.sub.CreateEntity(ctx, outsourcedEntity, doc)
	if err != nil {
		return Outsourced{}, classify(err, "")
	}
	out := outsourcedFromDoc(id, doc)
	if out.Responsible != nil {
		out.Responsible.Name = respName
	}
	return out, nil
}

// UpdateOutsourced updates an existing third-party provider (partial PATCH
// guarded by _version when present). Setting responsible_user_id to "" or "0"
// unlinks the representative account.
func (s *Service) UpdateOutsourced(ctx context.Context, in UpdateOutsourcedInput) (Outsourced, error) {
	if !positiveID(in.OutsourcedID) {
		return Outsourced{}, &ValidationError{Fields: []string{"outsourced_id"}, Reason: "must be a positive id"}
	}
	if in.Name == nil && in.Description == nil && in.ResponsibleUserID == nil && in.Signature == nil && in.SignatureClarification == nil {
		return Outsourced{}, validationFrom([]fieldProblem{{"outsourced_id", "provide at least one field to update"}})
	}

	var problems []fieldProblem
	var newName string
	if in.Name != nil {
		newName = strings.TrimSpace(*in.Name)
		if newName == "" {
			problems = append(problems, fieldProblem{"name", "must not be empty"})
		}
	}
	var setResp bool
	var newRespID int64
	if in.ResponsibleUserID != nil {
		setResp = true
		raw := strings.TrimSpace(string(*in.ResponsibleUserID))
		if raw != "" && raw != "0" {
			id, ok := parsePositiveID(*in.ResponsibleUserID)
			if !ok {
				problems = append(problems, fieldProblem{"responsible_user_id", "must be a positive id, or empty to unlink"})
			} else {
				newRespID = id
			}
		}
	}
	if err := validationFrom(problems); err != nil {
		return Outsourced{}, err
	}

	id := strings.TrimSpace(string(in.OutsourcedID))
	doc, err := s.sub.GetEntity(ctx, outsourcedEntity, id)
	if err != nil {
		if isMissing(err) {
			return Outsourced{}, &NotFoundError{ID: id, Kind: "outsourced provider"}
		}
		return Outsourced{}, classify(err, "")
	}

	var respName string
	if newRespID > 0 {
		names, userProblems, err := s.validateActiveUsers(ctx, map[string][]int64{"responsible_user_id": {newRespID}})
		if err != nil {
			return Outsourced{}, err
		}
		if err := validationFrom(userProblems); err != nil {
			return Outsourced{}, err
		}
		respName = names[strconv.FormatInt(newRespID, 10)]
	}

	patch := map[string]any{}
	if in.Name != nil {
		patch["name"] = newName
	}
	if in.Description != nil {
		patch["description"] = *in.Description
	}
	if setResp {
		patch["responsible_user_id"] = newRespID
	}
	if in.Signature != nil {
		patch["signature"] = *in.Signature
	}
	if in.SignatureClarification != nil {
		patch["signature_clarification"] = *in.SignatureClarification
	}
	if v := intOf(doc["_version"]); v > 0 {
		patch["_version"] = v
	}

	if err := s.sub.PatchEntity(ctx, outsourcedEntity, id, patch); err != nil {
		var xe *XoluError
		if errors.As(err, &xe) && xe.Status == 409 {
			return Outsourced{}, rejection("stale_state", "the outsourced provider changed while it was being updated: read it again and retry", "")
		}
		return Outsourced{}, classify(err, "")
	}
	for k, v := range patch {
		if k != "_version" {
			doc[k] = v
		}
	}
	out := outsourcedFromDoc(id, doc)
	if out.Responsible != nil {
		if respName != "" {
			out.Responsible.Name = respName
		} else {
			out.Responsible.Name = s.userNames(ctx)(out.Responsible.UserID)
		}
	}
	return out, nil
}

func parseDistinctPositiveIDs(field string, raw []ID, minCount int) ([]int64, []fieldProblem) {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(raw))
	for _, r := range raw {
		n, ok := parsePositiveID(r)
		if !ok {
			return nil, []fieldProblem{{field, fmt.Sprintf("%q is not a positive user id", string(r))}}
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) < minCount {
		return nil, []fieldProblem{{field, fmt.Sprintf("at least %d distinct members are required", minCount)}}
	}
	return out, nil
}

func (s *Service) validateActiveUsers(ctx context.Context, byField map[string][]int64) (map[string]string, []fieldProblem, error) {
	type cached struct {
		name    string
		problem string
	}
	cache := map[int64]cached{}
	names := map[string]string{}
	var problems []fieldProblem

	// Stable field order: responsible_user_id before member_user_ids.
	for _, field := range []string{"responsible_user_id", "member_user_ids"} {
		for _, uid := range byField[field] {
			c, ok := cache[uid]
			if !ok {
				idStr := strconv.FormatInt(uid, 10)
				doc, err := s.sub.GetEntity(ctx, usersEntity, idStr)
				if err != nil {
					if isMissing(err) {
						c = cached{problem: fmt.Sprintf("user %d does not exist", uid)}
					} else {
						return nil, nil, classify(err, "")
					}
				} else if active, hasActive := doc["active"].(bool); hasActive && !active {
					c = cached{problem: fmt.Sprintf("user %d is inactive", uid)}
				} else {
					n, _ := doc["name"].(string)
					c = cached{name: strings.TrimSpace(n)}
				}
				cache[uid] = c
			}
			if c.problem != "" {
				problems = append(problems, fieldProblem{field, c.problem})
			} else {
				names[strconv.FormatInt(uid, 10)] = c.name
			}
		}
	}
	return names, problems, nil
}

func ensureMemberIncluded(members []int64, uid int64) []int64 {
	if uid <= 0 || containsInt64(members, uid) {
		return members
	}
	return append(append([]int64(nil), members...), uid)
}

func containsInt64(xs []int64, want int64) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func int64IDList(v any) []int64 {
	raw := idList(v)
	out := make([]int64, 0, len(raw))
	for _, s := range raw {
		if n := intOf(s); n > 0 {
			out = append(out, n)
		}
	}
	return out
}

func crewWithNames(id string, doc map[string]any, names map[string]string) Crew {
	c := crewFromDoc(id, doc)
	if c.Responsible != nil {
		c.Responsible.Name = names[c.Responsible.UserID]
	}
	for i := range c.Members {
		c.Members[i].Name = names[c.Members[i].UserID]
	}
	return c
}
