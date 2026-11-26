package mcp

import (
	"fmt"
	"strings"

	"github.com/Gastonaso15/molu/pkg/help"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

// HelpToolName is the built-in tool that explains molu and the operations behind it.
const HelpToolName = "molu_help"

func helpToolDescriptor() ToolDescriptor {
	return ToolDescriptor{
		Name: HelpToolName,
		Description: "Explains how to use molu and how the operations of the system behind it work (the generic tools, the domain functions, errors). " +
			"Call it when you do not know how to do something or when a function rejects your request. Without a topic it returns an overview and the list of topics.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"topic": map[string]interface{}{
					"type":        "string",
					"description": "Optional topic name, as listed by the overview (for example primitives or errors). Omit it for the overview and the list of topics.",
				},
			},
			"additionalProperties": false,
		},
	}
}

// handleHelp serves one help topic. An unknown topic is not an error: the
// answer says so and lists the topics, which is what the caller needs next.
func (s *Server) handleHelp(args map[string]interface{}) (interface{}, *jsonRPCError) {
	topic := help.IndexTopic
	if raw, present := args["topic"]; present && raw != nil {
		t, ok := raw.(string)
		if !ok {
			te := taxonomy.NewContractViolation("", HelpToolName, []string{"topic"})
			s.incError(te.Code)
			return nil, typedRPCError(te)
		}
		if strings.TrimSpace(t) != "" {
			topic = help.Normalize(t)
		}
	}
	text, found := s.help.Get(topic)
	var b strings.Builder
	switch {
	case !found:
		fmt.Fprintf(&b, "There is no help topic %q.\n\n", topic)
	default:
		b.WriteString(text)
	}
	if !found || topic == help.IndexTopic {
		b.WriteString("\n\n## Topics\n")
		for _, t := range s.help.Topics() {
			if t.Summary != "" {
				fmt.Fprintf(&b, "- %s: %s\n", t.Name, t.Summary)
			} else {
				fmt.Fprintf(&b, "- %s\n", t.Name)
			}
		}
	}
	return map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": strings.TrimRight(b.String(), "\n")}},
	}, nil
}
