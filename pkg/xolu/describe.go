package xolu

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// describeScopes are the values the describe primitive accepts for scope.
var describeScopes = []string{"entities", "machines", "generators", "events"}

// executeDescribe serves the describe primitive: the semantic map of the xolu
// instance (§4 of the molu specification), read live from xolu.
//
//   - entities: GET /api/v1/entities, plus the JSON Schema from
//     GET /api/v1/schema/{entity} for the types that have one.
//   - machines: GET /api/v2/fsm/def and the definition of each machine.
//   - generators: GET /api/v2/gen/seq.
//   - events: GET /api/v2/event/def.
//
// scope "all" (the default) returns the four groups; a group xolu does not
// offer (a 404, as when /api/v2 is disabled) is reported under "unavailable"
// instead of failing the call. Any other failure, and a 404 on an explicitly
// requested scope, is returned as is. name keeps only the items called that
// (case-insensitive, matched against entity_type, name or id) and the
// per-item requests are made only for those.
func (c *HTTPXoluClient) executeDescribe(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	scope := "all"
	if s, ok := args["scope"].(string); ok && s != "" {
		scope = s
	}
	name := ""
	if n, ok := args["name"]; ok && n != nil {
		name = strings.TrimSpace(fmt.Sprint(n))
	}

	groups := describeScopes
	if scope != "all" {
		if !containsString(describeScopes, scope) {
			return nil, taxonomy.NewContractViolation("", "describe", []string{"scope"})
		}
		groups = []string{scope}
	}

	result := map[string]interface{}{}
	unavailable := map[string]interface{}{}
	found := false
	for _, group := range groups {
		items, err := c.describeGroup(ctx, group, name)
		if err != nil {
			var statusErr interface{ HTTPStatus() int }
			if scope == "all" && group != "entities" && errors.As(err, &statusErr) && statusErr.HTTPStatus() == 404 {
				unavailable[group] = err.Error()
				continue
			}
			return nil, err
		}
		result[group] = items
		if len(items) > 0 {
			found = true
		}
	}
	if len(unavailable) > 0 {
		result["unavailable"] = unavailable
	}
	if name != "" && !found {
		return nil, taxonomy.NewNotFound("", name)
	}
	return result, nil
}

func (c *HTTPXoluClient) describeGroup(ctx context.Context, group, name string) ([]interface{}, error) {
	switch group {
	case "entities":
		return c.describeEntities(ctx, name)
	case "machines":
		return c.describeMachines(ctx, name)
	case "generators":
		return c.describeList(ctx, "/api/v2/gen/seq", "sequences", name)
	default:
		return c.describeList(ctx, "/api/v2/event/def", "subscriptions", name)
	}
}

func (c *HTTPXoluClient) describeEntities(ctx context.Context, name string) ([]interface{}, error) {
	items, err := c.describeList(ctx, "/api/v1/entities", "entities", name)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		entity, ok := item.(map[string]interface{})
		if !ok || entity["has_schema"] != true {
			continue
		}
		var schema map[string]interface{}
		path := "/api/v1/schema/" + url.PathEscape(fmt.Sprint(entity["entity_type"]))
		if _, err := c.doRequest(ctx, "GET", path, nil, &schema); err != nil {
			return nil, err
		}
		entity["schema"] = schema
	}
	if name != "" {
		if err := c.addSuggestions(ctx, items); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// addSuggestions attaches, to each entity that has no registered schema, the
// schema xolu infers from its rows (GET /api/v1/entity/{type}/schema-suggestion).
// It is a guess from the data, not a contract, so it goes under "suggestion",
// marked inferred, and never under "schema". It costs one request per entity,
// so describe asks for it only when a name narrows the answer down.
// A suggestion xolu cannot make (an entity with no rows, a store that cannot
// infer) is reported as suggestion_unavailable; a substrate failure is not.
func (c *HTTPXoluClient) addSuggestions(ctx context.Context, items []interface{}) error {
	for _, item := range items {
		entity, ok := item.(map[string]interface{})
		if !ok || entity["has_schema"] == true {
			continue
		}
		var body map[string]interface{}
		path := "/api/v1/entity/" + url.PathEscape(fmt.Sprint(entity["entity_type"])) + "/schema-suggestion"
		if _, err := c.doRequest(ctx, "GET", path, nil, &body); err != nil {
			var te *taxonomy.TypedError
			if errors.As(err, &te) && strings.Contains(te.Code, string(taxonomy.CodeSubstrateUnavailable)) {
				return err
			}
			var he *HTTPError
			if te != nil || errors.As(err, &he) {
				entity["suggestion_unavailable"] = err.Error()
				continue
			}
			return err
		}
		entity["suggestion"] = map[string]interface{}{
			"inferred":         true,
			"sampled_rows":     body["sampled_rows"],
			"total_rows":       body["total_rows"],
			"suggested_schema": body["suggested_schema"],
			"field_analysis":   body["field_analysis"],
		}
	}
	return nil
}

func (c *HTTPXoluClient) describeMachines(ctx context.Context, name string) ([]interface{}, error) {
	items, err := c.describeList(ctx, "/api/v2/fsm/def", "definitions", name)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		machine, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		var def map[string]interface{}
		path := "/api/v2/fsm/def/" + url.PathEscape(fmt.Sprint(machine["id"]))
		if _, err := c.doRequest(ctx, "GET", path, nil, &def); err != nil {
			return nil, err
		}
		machine["spec"] = def["spec"]
	}
	return items, nil
}

// describeList reads a xolu listing and returns the items under key that match
// name (all of them when name is empty).
func (c *HTTPXoluClient) describeList(ctx context.Context, path, key, name string) ([]interface{}, error) {
	var body map[string]interface{}
	if _, err := c.doRequest(ctx, "GET", path, nil, &body); err != nil {
		return nil, err
	}
	all, _ := body[key].([]interface{})
	items := []interface{}{}
	for _, item := range all {
		if name == "" || describeMatches(item, name) {
			items = append(items, item)
		}
	}
	return items, nil
}

// describeMatches reports whether an item is called name: its entity_type,
// name or id equals it, ignoring case.
func describeMatches(item interface{}, name string) bool {
	m, ok := item.(map[string]interface{})
	if !ok {
		return false
	}
	for _, key := range []string{"entity_type", "name", "id"} {
		if v, ok := m[key]; ok && v != nil && strings.EqualFold(fmt.Sprint(v), name) {
			return true
		}
	}
	return false
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
