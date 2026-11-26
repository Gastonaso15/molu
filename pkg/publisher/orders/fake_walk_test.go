package orders

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// defStep applies input to a machine whose definition the fake knows (the
// template workflows). f.mu must be held.
func (f *fakeSubstrate) defStep(defID string, m *Machine, input string) (string, bool, error) {
	def := f.defs[defID]
	if m.Terminal {
		return "", false, &XoluError{Status: 409, Code: "XOLU-FSM003",
			Message: fmt.Sprintf("machine is in terminal state %q", m.State), State: m.State}
	}
	states, _ := def["states"].(map[string]any)
	transitions, _ := def["transitions"].([]any)
	for _, raw := range transitions {
		tr, _ := raw.(map[string]any)
		if tr["from"] == m.State && tr["input"] == input {
			to, _ := tr["to"].(string)
			st, _ := states[to].(map[string]any)
			terminal, _ := st["terminal"].(bool)
			return to, terminal, nil
		}
	}
	return "", false, &XoluError{Status: 409, Code: "XOLU-FSM003",
		Message: fmt.Sprintf("no transition for input %q from state %q", input, m.State), State: m.State}
}

// CommitWalk saves like Commit and moves the machine in the same step: a
// refused transition saves nothing, a refused commit moves nothing.
func (f *fakeSubstrate) CommitWalk(ctx context.Context, req CommitRequest) (string, error) {
	if req.FSMWalk == nil {
		return "", errors.New("CommitWalk without fsm_walk")
	}
	if f.failWith != nil {
		return "", f.failWith
	}
	id := strconv.FormatInt(req.FSMWalk.Machine, 10)
	f.mu.Lock()
	m, ok := f.machines[id]
	if !ok {
		f.mu.Unlock()
		return "", &XoluError{Status: 404, Code: "XOLU-FSM001", Message: "machine not found"}
	}
	var next string
	var terminal bool
	if defID, known := f.machineDef[id]; known {
		var err error
		if next, terminal, err = f.defStep(defID, m, req.FSMWalk.Input); err != nil {
			f.mu.Unlock()
			return "", err
		}
	} else {
		f.mu.Unlock()
		return "", errors.New("fake: machine without a known definition")
	}
	f.mu.Unlock()
	walk := req.FSMWalk
	req.FSMWalk = nil
	if err := f.Commit(ctx, req); err != nil {
		return "", err
	}
	f.mu.Lock()
	m.State, m.Terminal = next, terminal
	f.mu.Unlock()
	_ = walk
	return next, nil
}
