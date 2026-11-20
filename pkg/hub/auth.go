package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

var (
	// ErrUnauthorized indicates a missing or unrecognized publisher bearer token.
	ErrUnauthorized = errors.New("publisher token missing or invalid")
	// ErrNamespaceForbidden indicates a valid publisher token is not authorized
	// for the target namespace (§2.6).
	ErrNamespaceForbidden = errors.New("publisher token not authorized for namespace")
)

// PublisherAuthorizer enforces namespace-scoped authorization for publishers
// (§2.6). When no rules are configured (nil or empty map), it runs in open mode
// for local development compatibility.
type PublisherAuthorizer struct {
	rules map[string]map[string]struct{} // token -> allowed namespaces ("*" = any)
}

// NewPublisherAuthorizer builds an authorizer from a token -> namespaces map.
func NewPublisherAuthorizer(rules map[string][]string) *PublisherAuthorizer {
	if len(rules) == 0 {
		return &PublisherAuthorizer{}
	}
	copied := make(map[string]map[string]struct{}, len(rules))
	for tok, namespaces := range rules {
		set := make(map[string]struct{}, len(namespaces))
		for _, ns := range namespaces {
			if ns != "" {
				set[ns] = struct{}{}
			}
		}
		copied[tok] = set
	}
	return &PublisherAuthorizer{rules: copied}
}

// ParsePublisherTokens parses a JSON object mapping bearer tokens to allowed
// namespace slices: {"token-1": ["work_orders"], "admin-tok": ["*"]}.
func ParsePublisherTokens(data []byte) (map[string][]string, error) {
	var raw map[string][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("publisher tokens: invalid JSON: %w", err)
	}
	for tok, namespaces := range raw {
		if tok == "" {
			return nil, fmt.Errorf("publisher tokens: empty token key is not allowed")
		}
		if len(namespaces) == 0 {
			return nil, fmt.Errorf("publisher tokens: token must authorize at least one namespace")
		}
	}
	return raw, nil
}

// LoadPublisherAuthorizer loads rules from filePath (taking precedence) or
// inlineJSON. If both are empty, it returns an open-mode authorizer.
func LoadPublisherAuthorizer(filePath, inlineJSON string) (*PublisherAuthorizer, error) {
	var data []byte
	switch {
	case filePath != "":
		var err error
		data, err = os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("reading publisher tokens file %s: %w", filePath, err)
		}
	case inlineJSON != "":
		data = []byte(inlineJSON)
	default:
		return NewPublisherAuthorizer(nil), nil
	}
	rules, err := ParsePublisherTokens(data)
	if err != nil {
		return nil, err
	}
	return NewPublisherAuthorizer(rules), nil
}

// Enabled reports whether namespace authorization is active.
func (a *PublisherAuthorizer) Enabled() bool {
	return a != nil && len(a.rules) > 0
}

// Authorize checks that token is known and permitted to act on namespace.
func (a *PublisherAuthorizer) Authorize(token, namespace string) error {
	if !a.Enabled() {
		return nil
	}
	allowed, ok := a.rules[token]
	if !ok {
		return ErrUnauthorized
	}
	if _, anyNS := allowed["*"]; anyNS {
		return nil
	}
	if _, ok := allowed[namespace]; !ok {
		return ErrNamespaceForbidden
	}
	return nil
}

// ExtractBearerToken extracts the bearer token from an HTTP Authorization header.
func ExtractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
}
