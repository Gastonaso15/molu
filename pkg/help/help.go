// Package help serves the texts behind the molu_help tool: short, topic by
// topic explanations that tell an agent what molu is, how the operations of
// the system behind it work and what to do when it does not know yet. The
// texts are Markdown files. The ones compiled into the binary are generic (the
// tool, the primitives, the errors) and know nothing of any domain; the domain
// texts belong to each publisher and are loaded from directories, which can
// also override any default or add topics, without recompiling.
package help

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed texts/*.md
var embedded embed.FS

// IndexTopic is the topic served when the caller names none.
const IndexTopic = "overview"

// Topic names one help text with its one-line summary.
type Topic struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

// Source serves help texts by topic.
type Source interface {
	// Topics lists every topic, the overview first, the rest by name.
	Topics() []Topic
	// Get returns the text of a topic (without its summary line).
	Get(topic string) (string, bool)
}

// FS is a Source over a directory tree of <topic>.md files.
type FS struct{ fsys fs.FS }

// Embedded returns the default texts compiled into the binary.
func Embedded() *FS {
	sub, err := fs.Sub(embedded, "texts")
	if err != nil {
		panic(err) // the embedded directory is fixed at build time
	}
	return &FS{fsys: sub}
}

// Dirs layers directories over the defaults: later directories win over
// earlier ones, and all of them win over the defaults. Every entry must be an
// existing directory.
func Dirs(dirs []string, defaults Source) (Source, error) {
	src := defaults
	for _, d := range dirs {
		next, err := Dir(d, src)
		if err != nil {
			return nil, err
		}
		src = next
	}
	return src, nil
}

// Dir returns the texts of a directory. A topic that exists in both the
// directory and the defaults is served from the directory.
func Dir(dir string, defaults Source) (Source, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New(dir + " is not a directory")
	}
	return &overlay{top: &FS{fsys: os.DirFS(dir)}, base: defaults}, nil
}

type overlay struct{ top, base Source }

func (o *overlay) Topics() []Topic {
	byName := map[string]Topic{}
	for _, t := range o.base.Topics() {
		byName[t.Name] = t
	}
	for _, t := range o.top.Topics() {
		byName[t.Name] = t
	}
	return sortTopics(byName)
}

func (o *overlay) Get(topic string) (string, bool) {
	if s, ok := o.top.Get(topic); ok {
		return s, true
	}
	return o.base.Get(topic)
}

func (f *FS) Topics() []Topic {
	byName := map[string]Topic{}
	entries, _ := fs.ReadDir(f.fsys, ".")
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || e.IsDir() {
			continue
		}
		raw, err := fs.ReadFile(f.fsys, e.Name())
		if err != nil {
			continue
		}
		summary, _ := split(string(raw))
		byName[name] = Topic{Name: name, Summary: summary}
	}
	return sortTopics(byName)
}

func (f *FS) Get(topic string) (string, bool) {
	topic = Normalize(topic)
	if topic == "" || strings.ContainsAny(topic, `/\`) || strings.Contains(topic, "..") {
		return "", false
	}
	raw, err := fs.ReadFile(f.fsys, filepath.ToSlash(topic)+".md")
	if err != nil {
		return "", false
	}
	_, body := split(string(raw))
	return body, true
}

// Normalize lowers a topic name and trims it, so "Work_Orders " finds work_orders.
func Normalize(topic string) string { return strings.ToLower(strings.TrimSpace(topic)) }

// split separates the optional first line "summary: ..." from the body.
func split(text string) (summary, body string) {
	first, rest, found := strings.Cut(text, "\n")
	if s, ok := strings.CutPrefix(strings.TrimSpace(first), "summary:"); ok && found {
		return strings.TrimSpace(s), strings.TrimLeft(rest, "\n")
	}
	return "", text
}

func sortTopics(byName map[string]Topic) []Topic {
	out := make([]Topic, 0, len(byName))
	for _, t := range byName {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == IndexTopic) != (out[j].Name == IndexTopic) {
			return out[i].Name == IndexTopic
		}
		return out[i].Name < out[j].Name
	})
	return out
}
