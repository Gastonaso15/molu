package hub

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublisherAuthorizer_OpenMode(t *testing.T) {
	auth := NewPublisherAuthorizer(nil)
	if auth.Enabled() {
		t.Fatal("expected open mode (Enabled=false) for nil rules")
	}
	if err := auth.Authorize("", "work_orders"); err != nil {
		t.Fatalf("open mode must allow any request, got %v", err)
	}
}

func TestPublisherAuthorizer_RulesAndWildcard(t *testing.T) {
	rules, err := ParsePublisherTokens([]byte(`{
		"tok-orders": ["work_orders"],
		"tok-admin": ["*"]
	}`))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	auth := NewPublisherAuthorizer(rules)
	if !auth.Enabled() {
		t.Fatal("expected Enabled=true")
	}

	if err := auth.Authorize("", "work_orders"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("empty token: want ErrUnauthorized, got %v", err)
	}
	if err := auth.Authorize("wrong", "work_orders"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unknown token: want ErrUnauthorized, got %v", err)
	}
	if err := auth.Authorize("tok-orders", "issues"); !errors.Is(err, ErrNamespaceForbidden) {
		t.Fatalf("disallowed namespace: want ErrNamespaceForbidden, got %v", err)
	}
	if err := auth.Authorize("tok-orders", "work_orders"); err != nil {
		t.Fatalf("allowed namespace: unexpected error %v", err)
	}
	if err := auth.Authorize("tok-admin", "anything"); err != nil {
		t.Fatalf("wildcard token: unexpected error %v", err)
	}
}

func TestPublisherAuthorizer_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	if err := os.WriteFile(path, []byte(`{"tok-1":["ns_a","ns_b"]}`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	auth, err := LoadPublisherAuthorizer(path, "")
	if err != nil {
		t.Fatalf("LoadPublisherAuthorizer error: %v", err)
	}
	if err := auth.Authorize("tok-1", "ns_b"); err != nil {
		t.Fatalf("expected ns_b allowed, got %v", err)
	}
	if err := auth.Authorize("tok-1", "ns_c"); !errors.Is(err, ErrNamespaceForbidden) {
		t.Fatalf("expected ErrNamespaceForbidden for ns_c, got %v", err)
	}
}
