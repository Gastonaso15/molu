package orders

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// blockLineKey is the key Seam stores one block line's answer under in the
// order's block_values: the block's and the line's position in the template.
func blockLineKey(block, line int) string {
	return fmt.Sprintf("template_block_%d_line_%d", block, line)
}

var blockLineKeyRE = regexp.MustCompile(`^template_block_(\d+)_line_(\d+)$`)

func normName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// blockAnswers merges the answers given by block and line name into the ones
// already stored (base, keyed the way Seam keys them). Each answer is checked
// against its line's type, with the same rules as a header item. Lines are
// optional, as in Seam's form: an answer that is missing or blank keeps what is
// stored. Unknown blocks or lines and wrongly typed answers are reported
// per field.
func (s *Service) blockAnswers(ctx context.Context, tpl workOrderTemplate, supplied, base map[string]any) (map[string]any, []fieldProblem, error) {
	out := make(map[string]any, len(base)+len(supplied))
	for k, v := range base {
		out[k] = v
	}
	if len(supplied) == 0 {
		return out, nil, nil
	}
	if len(tpl.Blocks) == 0 {
		return out, []fieldProblem{{"block_values", "the template has no blocks"}}, nil
	}
	var problems []fieldProblem
	for _, blockName := range sortedKeys(supplied) {
		field := "block_values." + blockName
		bi := -1
		for i, b := range tpl.Blocks {
			if normName(b.Name) == normName(blockName) {
				bi = i
			}
		}
		if bi < 0 {
			problems = append(problems, fieldProblem{field, "not a block of this template"})
			continue
		}
		var lines map[string]any
		if err := roundTrip(supplied[blockName], &lines); err != nil {
			problems = append(problems, fieldProblem{field, "must be an object with the answers by line name"})
			continue
		}
		for _, lineName := range sortedKeys(lines) {
			lineField := field + "." + lineName
			li := -1
			for j, l := range tpl.Blocks[bi].Lines {
				if normName(l.Name) == normName(lineName) {
					li = j
				}
			}
			if li < 0 {
				problems = append(problems, fieldProblem{lineField, "not a line of this block"})
				continue
			}
			raw := lines[lineName]
			if isBlank(raw) {
				continue
			}
			typed, msg, err := s.typedHeaderValue(ctx, headerItem{Type: tpl.Blocks[bi].Lines[li].Type}, raw)
			if err != nil {
				return nil, nil, err
			}
			if msg != "" {
				problems = append(problems, fieldProblem{lineField, msg})
				continue
			}
			out[blockLineKey(bi, li)] = typed
		}
	}
	return out, problems, nil
}

// decodeBlockAnswers turns the stored block_values back into answers by block
// and line name. Keys that do not point at a block and line of the template are
// left out.
func decodeBlockAnswers(tpl workOrderTemplate, values map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for key, v := range values {
		m := blockLineKeyRE.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		bi, _ := strconv.Atoi(m[1])
		li, _ := strconv.Atoi(m[2])
		if bi >= len(tpl.Blocks) || li >= len(tpl.Blocks[bi].Lines) {
			continue
		}
		name := strings.TrimSpace(tpl.Blocks[bi].Name)
		if out[name] == nil {
			out[name] = map[string]any{}
		}
		out[name][strings.TrimSpace(tpl.Blocks[bi].Lines[li].Name)] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
