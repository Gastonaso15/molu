package help

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedTopicsStartWithTheOverviewAndHaveSummaries(t *testing.T) {
	topics := Embedded().Topics()
	if len(topics) != 3 || topics[0].Name != IndexTopic {
		t.Fatalf("topics = %+v", topics)
	}
	names := map[string]bool{}
	for _, tp := range topics {
		names[tp.Name] = true
		if tp.Summary == "" {
			t.Errorf("topic %s has no summary", tp.Name)
		}
		body, ok := Embedded().Get(tp.Name)
		if !ok || strings.TrimSpace(body) == "" || strings.HasPrefix(body, "summary:") {
			t.Errorf("topic %s body = %q ok=%v", tp.Name, body, ok)
		}
	}
	for _, want := range []string{"overview", "primitives", "errors"} {
		if !names[want] {
			t.Errorf("missing topic %s", want)
		}
	}
}

// The overview names every topic it points to, so an agent can follow it.
func TestOverviewMentionsEveryEmbeddedTopic(t *testing.T) {
	body, _ := Embedded().Get(IndexTopic)
	for _, tp := range Embedded().Topics() {
		if tp.Name != IndexTopic && !strings.Contains(body, "`"+tp.Name+"`") {
			t.Errorf("the overview does not mention topic %s", tp.Name)
		}
	}
}

func TestGetNormalizesAndRefusesPathsAndUnknownTopics(t *testing.T) {
	if _, ok := Embedded().Get("  Primitives "); !ok {
		t.Error("topic lookup should ignore case and spaces")
	}
	for _, bad := range []string{"", "../x", "a/b", `a\b`, "nope", ".."} {
		if _, ok := Embedded().Get(bad); ok {
			t.Errorf("Get(%q) should fail", bad)
		}
	}
}

func TestDirOverridesAndAddsTopics(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "errors.md"), []byte("summary: errores de la casa\nTexto propio.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "cliente_x.md"), []byte("summary: reglas del cliente X\nUsa siempre el depósito 3.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ignorado.txt"), []byte("no es un tema"), 0o644)
	src, err := Dir(dir, Embedded())
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := src.Get("errors"); !strings.HasPrefix(body, "Texto propio.") {
		t.Errorf("errors should come from the directory: %q", body)
	}
	if body, ok := src.Get("cliente_x"); !ok || !strings.Contains(body, "depósito 3") {
		t.Errorf("cliente_x = %q ok=%v", body, ok)
	}
	if _, ok := src.Get("ignorado"); ok {
		t.Error("only .md files are topics")
	}
	if body, ok := src.Get("primitives"); !ok || body == "" {
		t.Error("topics the directory lacks fall back to the defaults")
	}
	got := map[string]string{}
	for _, tp := range src.Topics() {
		got[tp.Name] = tp.Summary
	}
	if got["errors"] != "errores de la casa" || got["cliente_x"] == "" || got["overview"] == "" {
		t.Errorf("topics = %v", got)
	}
	if src.Topics()[0].Name != IndexTopic {
		t.Error("the overview must stay first")
	}
}

func TestDirRejectsAMissingDirectoryAndAFile(t *testing.T) {
	if _, err := Dir(filepath.Join(t.TempDir(), "no-existe"), Embedded()); err == nil {
		t.Error("a missing directory must be an error")
	}
	f := filepath.Join(t.TempDir(), "f")
	os.WriteFile(f, []byte("x"), 0o644)
	if _, err := Dir(f, Embedded()); err == nil {
		t.Error("a file is not a directory")
	}
}

// The embedded texts are generic: they name no domain and no product.
func TestEmbeddedTextsAreDomainNeutral(t *testing.T) {
	for _, tp := range Embedded().Topics() {
		body, _ := Embedded().Get(tp.Name)
		low := strings.ToLower(body + tp.Summary)
		for _, banned := range []string{"seam", "work_orders", "workorders", "issues."} {
			if strings.Contains(low, banned) {
				t.Errorf("topic %s mentions %q", tp.Name, banned)
			}
		}
	}
}

// The domain texts ship with their publishers and layer over the defaults.
func TestPublisherHelpDirectoriesLoadOverTheDefaults(t *testing.T) {
	src, err := Dirs([]string{"../publisher/orders/help", "../publisher/issues/help"}, Embedded())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tp := range src.Topics() {
		got[tp.Name] = true
		if tp.Summary == "" {
			t.Errorf("topic %s has no summary", tp.Name)
		}
	}
	for _, want := range []string{"overview", "primitives", "errors", "work_orders", "work_orders.execution", "work_orders.inventory", "issues"} {
		if !got[want] {
			t.Errorf("missing topic %s (have %v)", want, got)
		}
	}
	if body, _ := src.Get("work_orders.execution"); !strings.Contains(body, "ConfirmExecutionHeader") {
		t.Error("the execution topic should explain the header step")
	}
	if _, err := Dirs([]string{"../publisher/orders/help", "no-existe"}, Embedded()); err == nil {
		t.Error("a missing directory in the list must be an error")
	}
}
