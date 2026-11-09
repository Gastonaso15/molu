package orders

import "testing"

// xolu rejects definitions without an explicit determinism level (XOLU-FSM006).
func TestLifecycleDefinition_DeclaresDeterminismAndAlphabet(t *testing.T) {
	def := LifecycleDefinition()
	if def["determinism"] != "strict" {
		t.Errorf("determinism = %v, want strict", def["determinism"])
	}
	if a, _ := def["output_alphabet"].([]any); len(a) != 1 || a[0] != "order_completed" {
		t.Errorf("output_alphabet = %v", def["output_alphabet"])
	}
}
