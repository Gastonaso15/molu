package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/help"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

type failingHealth struct{}

func (failingHealth) Check() error { return errors.New("xolu is down") }

func helpText(t *testing.T, reply toolCallReply) string {
	t.Helper()
	if reply.Error != nil || reply.Result == nil || reply.Result.IsError || len(reply.Result.Content) != 1 {
		t.Fatalf("expected one text result, got %+v / %+v", reply.Result, reply.Error)
	}
	return reply.Result.Content[0].Text
}

func TestMoluHelpIsListedOnlyWhenEnabled(t *testing.T) {
	on := NewServer(ServerConfig{Help: help.Embedded()})
	off := NewServer(ServerConfig{})
	has := func(s *Server) bool {
		for _, tl := range s.HandleListTools(context.Background()) {
			if tl.Name == HelpToolName {
				return true
			}
		}
		return false
	}
	if !has(on) || has(off) {
		t.Fatalf("listed: enabled=%v disabled=%v", has(on), has(off))
	}
	// The generic primitives are unchanged: help is one more tool, not a replacement.
	if len(on.HandleListTools(context.Background())) != len(off.HandleListTools(context.Background()))+1 {
		t.Error("molu_help should add exactly one tool")
	}
	te := typedFromToolError(t, callTool(t, off, `{"name":"molu_help","arguments":{}}`))
	if taxonomy.StripPrefix(te.Code) != taxonomy.CodeNotOffered {
		t.Errorf("a disabled help tool must be NOT_OFFERED, got %s", te.Code)
	}
}

func TestMoluHelpAnswersWithTheOverviewAndTheTopics(t *testing.T) {
	srv := NewServer(ServerConfig{Help: help.Embedded()})
	for _, params := range []string{`{"name":"molu_help","arguments":{}}`, `{"name":"molu_help"}`, `{"name":"molu_help","arguments":{"topic":""}}`, `{"name":"molu_help","arguments":{"topic":null}}`} {
		text := helpText(t, callTool(t, srv, params))
		if !strings.Contains(text, "# molu") || !strings.Contains(text, "## Topics") || !strings.Contains(text, "- primitives: ") {
			t.Errorf("%s -> %q", params, text)
		}
	}
}

func TestMoluHelpTopicsUnknownTopicsAndBadArguments(t *testing.T) {
	srv := NewServer(ServerConfig{Help: help.Embedded()})
	text := helpText(t, callTool(t, srv, `{"name":"molu_help","arguments":{"topic":" Primitives "}}`))
	if !strings.Contains(text, "centésimas") || strings.Contains(text, "## Topics") {
		t.Errorf("topic text = %q", text)
	}
	// An unknown topic is an answer that lists the topics, not a failure.
	text = helpText(t, callTool(t, srv, `{"name":"molu_help","arguments":{"topic":"nope"}}`))
	if !strings.Contains(text, `no help topic "nope"`) || !strings.Contains(text, "- errors: ") {
		t.Errorf("unknown topic = %q", text)
	}
	te := typedFromToolError(t, callTool(t, srv, `{"name":"molu_help","arguments":{"topic":5}}`))
	if taxonomy.StripPrefix(te.Code) != taxonomy.CodeContractViolation {
		t.Errorf("a non-string topic must be CONTRACT_VIOLATION, got %s", te.Code)
	}
}

func TestMoluHelpWorksWhileTheSubstrateIsDown(t *testing.T) {
	srv := NewServer(ServerConfig{Help: help.Embedded(), Health: failingHealth{}})
	if text := helpText(t, callTool(t, srv, `{"name":"molu_help","arguments":{"topic":"errors"}}`)); !strings.Contains(text, "INVALID_TRANSITION") {
		t.Errorf("errors topic = %q", text)
	}
	// The generic primitives still go through the health gate.
	te := typedFromToolError(t, callTool(t, srv, `{"name":"describe","arguments":{}}`))
	if taxonomy.StripPrefix(te.Code) != taxonomy.CodeSubstrateUnavailable {
		t.Errorf("describe should still be gated, got %s", te.Code)
	}
}

func TestMoluHelpDescriptorHasAnOptionalTopicOnly(t *testing.T) {
	d := helpToolDescriptor()
	raw, _ := json.Marshal(d.InputSchema)
	var schema struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
		Additional bool           `json:"additionalProperties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 0 || len(schema.Properties) != 1 || schema.Properties["topic"] == nil || schema.Additional {
		t.Errorf("schema = %s", raw)
	}
}
