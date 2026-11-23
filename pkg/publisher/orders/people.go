package orders

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

const (
	usersEntity     = "users"
	catalogScanSize = 500 // rows read to filter a small catalog in memory
)

// Person is a user referenced by a crew or a third party. Name is resolved
// only by the Get functions, and only when the user can be read.
type Person struct {
	UserID string `json:"user_id"`
	Name   string `json:"name,omitempty"`
}

// Crew is an internal team: a responsible user (who may claim job-board orders
// for it) and its members.
type Crew struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Responsible *Person  `json:"responsible,omitempty"`
	Members     []Person `json:"members"`
}

// ListCrewsInput filters work_orders.ListCrews; filters combine with AND.
type ListCrewsInput struct {
	Name              string `json:"name,omitempty"` // contains, ignoring case
	ResponsibleUserID ID     `json:"responsible_user_id,omitempty"`
	MemberUserID      ID     `json:"member_user_id,omitempty"`
	Limit             int    `json:"limit,omitempty"`
}

// CrewList is the result of work_orders.ListCrews, sorted by name.
type CrewList struct {
	Crews     []Crew `json:"crews"`
	Total     int    `json:"total"`
	Truncated bool   `json:"truncated,omitempty"`
}

// GetCrewInput is the payload of work_orders.GetCrew.
type GetCrewInput struct {
	CrewID ID `json:"crew_id"`
}

// Outsourced is an external provider (a third party) that can be assigned
// orders. The signature image is not returned; HasSignature says one is stored.
type Outsourced struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	Description            string  `json:"description,omitempty"`
	Responsible            *Person `json:"responsible,omitempty"` // representative account
	SignatureClarification string  `json:"signature_clarification,omitempty"`
	HasSignature           bool    `json:"has_signature"`
}

// ListOutsourcedInput filters work_orders.ListOutsourced.
type ListOutsourcedInput struct {
	Name              string `json:"name,omitempty"`
	ResponsibleUserID ID     `json:"responsible_user_id,omitempty"`
	Limit             int    `json:"limit,omitempty"`
}

// OutsourcedList is the result of work_orders.ListOutsourced, sorted by name.
type OutsourcedList struct {
	Outsourced []Outsourced `json:"outsourced"`
	Total      int          `json:"total"`
	Truncated  bool         `json:"truncated,omitempty"`
}

// GetOutsourcedInput is the payload of work_orders.GetOutsourced.
type GetOutsourcedInput struct {
	OutsourcedID ID `json:"outsourced_id"`
}

func positiveID(id ID) bool {
	n, err := strconv.ParseInt(strings.TrimSpace(string(id)), 10, 64)
	return err == nil && n > 0
}

// optionalIDProblem reports a filter id that is present but not a positive id.
func optionalIDProblem(field string, id ID) []fieldProblem {
	if id != "" && !positiveID(id) {
		return []fieldProblem{{field, "must be a positive id"}}
	}
	return nil
}

func validLimit(limit int) (int, []fieldProblem) {
	switch {
	case limit < 0 || limit > maxListLimit:
		return 0, []fieldProblem{{"limit", "must be between 1 and 200"}}
	case limit == 0:
		return defaultListLimit, nil
	}
	return limit, nil
}

// scanCatalog reads up to catalogScanSize documents of a small catalog entity
// (the way Seam's own lists do) so the caller can filter and sort them. An
// entity that does not exist yet is an empty catalog.
func (s *Service) scanCatalog(ctx context.Context, entity string) (rows []map[string]any, truncated bool, err error) {
	rows, err = s.sub.FindEntities(ctx, entity, EntityQuery{Limit: catalogScanSize, OrderBy: []string{"id"}})
	if err != nil {
		if isMissing(err) {
			return nil, false, nil
		}
		return nil, false, classify(err, "")
	}
	return rows, len(rows) >= catalogScanSize, nil
}

func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(strings.TrimSpace(needle)))
}

func personFrom(v any) *Person {
	if id := idString(v); id != "" && id != "0" {
		return &Person{UserID: id}
	}
	return nil
}

func crewFromDoc(id string, doc map[string]any) Crew {
	c := Crew{ID: id, Members: []Person{}}
	c.Name, _ = doc["name"].(string)
	c.Description, _ = doc["description"].(string)
	c.Responsible = personFrom(doc["responsible_user_id"])
	for _, m := range idList(doc["members"]) {
		if m != "0" {
			c.Members = append(c.Members, Person{UserID: m})
		}
	}
	return c
}

func outsourcedFromDoc(id string, doc map[string]any) Outsourced {
	o := Outsourced{ID: id}
	o.Name, _ = doc["name"].(string)
	o.Description, _ = doc["description"].(string)
	o.Responsible = personFrom(doc["responsible_user_id"])
	o.SignatureClarification, _ = doc["signature_clarification"].(string)
	sig, _ := doc["signature"].(string)
	o.HasSignature = strings.TrimSpace(sig) != ""
	return o
}

func (c Crew) hasMember(userID string) bool {
	for _, m := range c.Members {
		if m.UserID == userID {
			return true
		}
	}
	return false
}

// ListCrews lists the crews sorted by name. User names are not resolved here;
// GetCrew does that for one crew.
func (s *Service) ListCrews(ctx context.Context, in ListCrewsInput) (CrewList, error) {
	limit, problems := validLimit(in.Limit)
	problems = append(problems, optionalIDProblem("responsible_user_id", in.ResponsibleUserID)...)
	problems = append(problems, optionalIDProblem("member_user_id", in.MemberUserID)...)
	if err := validationFrom(problems); err != nil {
		return CrewList{}, err
	}
	rows, truncated, err := s.scanCatalog(ctx, crewEntity)
	if err != nil {
		return CrewList{}, err
	}
	var crews []Crew
	for _, r := range rows {
		c := crewFromDoc(idString(r["id"]), r)
		switch {
		case in.Name != "" && !containsFold(c.Name, in.Name):
		case in.ResponsibleUserID != "" && (c.Responsible == nil || c.Responsible.UserID != string(in.ResponsibleUserID)):
		case in.MemberUserID != "" && !c.hasMember(string(in.MemberUserID)):
		default:
			crews = append(crews, c)
		}
	}
	sort.Slice(crews, func(i, j int) bool {
		if crews[i].Name != crews[j].Name {
			return crews[i].Name < crews[j].Name
		}
		return intOf(crews[i].ID) < intOf(crews[j].ID)
	})
	out := CrewList{Crews: []Crew{}, Truncated: truncated}
	if len(crews) > limit {
		crews, out.Truncated = crews[:limit], true
	}
	out.Crews = append(out.Crews, crews...)
	out.Total = len(out.Crews)
	return out, nil
}

// GetCrew returns one crew with the names of its responsible user and members.
// Names come from the users entity; a user that cannot be read keeps an empty
// name. Nothing else of the user is returned.
func (s *Service) GetCrew(ctx context.Context, in GetCrewInput) (Crew, error) {
	if !positiveID(in.CrewID) {
		return Crew{}, &ValidationError{Fields: []string{"crew_id"}, Reason: "must be a positive id"}
	}
	id := strings.TrimSpace(string(in.CrewID))
	doc, err := s.sub.GetEntity(ctx, crewEntity, id)
	if err != nil {
		if isMissing(err) {
			return Crew{}, &NotFoundError{ID: id, Kind: "crew"}
		}
		return Crew{}, classify(err, "")
	}
	c := crewFromDoc(id, doc)
	names := s.userNames(ctx)
	if c.Responsible != nil {
		c.Responsible.Name = names(c.Responsible.UserID)
	}
	for i := range c.Members {
		c.Members[i].Name = names(c.Members[i].UserID)
	}
	return c, nil
}

// userNames returns a cached lookup of user names. Only the name is read from
// a user document; failures give an empty name.
func (s *Service) userNames(ctx context.Context) func(id string) string {
	cache := map[string]string{}
	return func(id string) string {
		if name, ok := cache[id]; ok {
			return name
		}
		var name string
		if doc, err := s.sub.GetEntity(ctx, usersEntity, id); err == nil {
			name, _ = doc["name"].(string)
		}
		cache[id] = name
		return name
	}
}

// ListOutsourced lists the third-party providers sorted by name.
func (s *Service) ListOutsourced(ctx context.Context, in ListOutsourcedInput) (OutsourcedList, error) {
	limit, problems := validLimit(in.Limit)
	problems = append(problems, optionalIDProblem("responsible_user_id", in.ResponsibleUserID)...)
	if err := validationFrom(problems); err != nil {
		return OutsourcedList{}, err
	}
	rows, truncated, err := s.scanCatalog(ctx, outsourcedEntity)
	if err != nil {
		return OutsourcedList{}, err
	}
	var list []Outsourced
	for _, r := range rows {
		o := outsourcedFromDoc(idString(r["id"]), r)
		switch {
		case in.Name != "" && !containsFold(o.Name, in.Name):
		case in.ResponsibleUserID != "" && (o.Responsible == nil || o.Responsible.UserID != string(in.ResponsibleUserID)):
		default:
			list = append(list, o)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return intOf(list[i].ID) < intOf(list[j].ID)
	})
	out := OutsourcedList{Outsourced: []Outsourced{}, Truncated: truncated}
	if len(list) > limit {
		list, out.Truncated = list[:limit], true
	}
	out.Outsourced = append(out.Outsourced, list...)
	out.Total = len(out.Outsourced)
	return out, nil
}

// GetOutsourced returns one third-party provider with the name of its
// representative account.
func (s *Service) GetOutsourced(ctx context.Context, in GetOutsourcedInput) (Outsourced, error) {
	if !positiveID(in.OutsourcedID) {
		return Outsourced{}, &ValidationError{Fields: []string{"outsourced_id"}, Reason: "must be a positive id"}
	}
	id := strings.TrimSpace(string(in.OutsourcedID))
	doc, err := s.sub.GetEntity(ctx, outsourcedEntity, id)
	if err != nil {
		if isMissing(err) {
			return Outsourced{}, &NotFoundError{ID: id, Kind: "outsourced provider"}
		}
		return Outsourced{}, classify(err, "")
	}
	o := outsourcedFromDoc(id, doc)
	if o.Responsible != nil {
		o.Responsible.Name = s.userNames(ctx)(o.Responsible.UserID)
	}
	return o, nil
}
