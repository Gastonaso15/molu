package taxonomy

import (
	"encoding/json"
	"testing"
)

func TestNewNotOffered(t *testing.T) {
	err := NewNotOffered("tenant-123", "work_orders.CreateOrder")
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeNotOffered))
	requireMessageContains(t, err, "tenant-123")
	requireMessageContains(t, err, "work_orders.CreateOrder")
}

func TestNewNotFound(t *testing.T) {
	err := NewNotFound("tenant-123", "order-456")
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeNotFound))
	requireMessageContains(t, err, "tenant-123")
	requireMessageContains(t, err, "order-456")
}

func TestNewEmpty(t *testing.T) {
	err := NewEmpty("tenant-123", "find", map[string]any{"status": "open"})
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeEmpty))
	requireMessageContains(t, err, "tenant-123")
	requireMessageContains(t, err, "find")
}

func TestNewContractViolation(t *testing.T) {
	err := NewContractViolation("tenant-123", "work_orders.CreateOrder", []string{"title", "assignee"})
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeContractViolation))
	requireMessageContains(t, err, "tenant-123")
	requireDetailsFields(t, err, []string{"title", "assignee"})
}

func TestNewSubstrateUnavailable(t *testing.T) {
	err := NewSubstrateUnavailable("2026-01-01T10:00:00Z", "2026-01-01T10:30:00Z", 5, 10)
	requireNotNil(t, err)
	requireCode(t, err, PrefixMolu+string(CodeSubstrateUnavailable))
	requireDetailsRetryMetadata(t, err, "2026-01-01T10:00:00Z", "2026-01-01T10:30:00Z", 5, 10)
}

func requireNotNil(t *testing.T, err *TypedError) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func requireCode(t *testing.T, err *TypedError, expectedCode string) {
	t.Helper()
	if err.Code != expectedCode {
		t.Errorf("code = %q, want %q", err.Code, expectedCode)
	}
}

func requireMessageContains(t *testing.T, err *TypedError, substr string) {
	t.Helper()
	if err.Message == "" || !contains(err.Message, substr) {
		t.Errorf("message %q does not contain %q", err.Message, substr)
	}
}

func requireDetailsFields(t *testing.T, err *TypedError, fields []string) {
	t.Helper()
	if err.Details == nil {
		t.Fatal("details is nil")
	}
	fieldsVal, ok := err.Details["fields"].([]string)
	if !ok {
		t.Fatalf("details.fields not []string: %v", err.Details["fields"])
	}
	if len(fieldsVal) != len(fields) {
		t.Errorf("fields = %v, want %v", fieldsVal, fields)
	}
}

func requireDetailsRetryMetadata(t *testing.T, err *TypedError, lastContact, nextRetry string, attempt, max int) {
	t.Helper()
	if err.Details == nil {
		t.Fatal("details is nil")
	}
	if err.Details["lastSuccessfulContact"] != lastContact {
		t.Errorf("lastSuccessfulContact = %v, want %v", err.Details["lastSuccessfulContact"], lastContact)
	}
	if err.Details["nextRetryAt"] != nextRetry {
		t.Errorf("nextRetryAt = %v, want %v", err.Details["nextRetryAt"], nextRetry)
	}
	if err.Details["attemptNumber"] != attempt {
		t.Errorf("attemptNumber = %v, want %v", err.Details["attemptNumber"], attempt)
	}
	if err.Details["maxAttempts"] != max {
		t.Errorf("maxAttempts = %v, want %v", err.Details["maxAttempts"], max)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestTypedError_JSONSerialization(t *testing.T) {
	err := &TypedError{
		Code:    "XOLU-MOLU-FRONT-NOT_FOUND",
		Message: "not found",
		Details: map[string]any{"foo": "bar"},
	}
	data, errMarshal := json.Marshal(err)
	if errMarshal != nil {
		t.Fatalf("marshal: %v", errMarshal)
	}
	var decoded TypedError
	if errUnmarshal := json.Unmarshal(data, &decoded); errUnmarshal != nil {
		t.Fatalf("unmarshal: %v", errUnmarshal)
	}
	if decoded.Code != err.Code || decoded.Message != err.Message {
		t.Errorf("roundtrip mismatch: got %+v", decoded)
	}
}

func TestWrapXoluNotFound(t *testing.T) {
	xoluErr := &TypedError{Code: PrefixXolu + string(CodeNotFound), Message: "xolu not found"}
	wrapped := WrapXoluNotFound(xoluErr)
	requireNotNil(t, wrapped)
	requireCode(t, wrapped, PrefixXolu+string(CodeNotFound))
	requireMessageContains(t, wrapped, "xolu not found")
}

func TestWrapXoluNotFound_NilReturnsNil(t *testing.T) {
	if WrapXoluNotFound(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestWrapXoluNotFound_WrongCodeReturnsNil(t *testing.T) {
	xoluErr := &TypedError{Code: PrefixXolu + string(CodeEmpty), Message: "empty"}
	if WrapXoluNotFound(xoluErr) != nil {
		t.Error("expected nil for wrong code")
	}
}

func TestWrapXoluInvalidTransition(t *testing.T) {
	xoluErr := &TypedError{Code: PrefixXolu + string(CodeInvalidTransition), Message: "guard: invalid state"}
	wrapped := WrapXoluInvalidTransition(xoluErr)
	requireNotNil(t, wrapped)
	requireCode(t, wrapped, PrefixXolu+string(CodeInvalidTransition))
	requireMessageContains(t, wrapped, "guard: invalid state")
}

func TestWrapXoluInvalidTransition_NilReturnsNil(t *testing.T) {
	if WrapXoluInvalidTransition(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestWrapXoluInvalidTransition_WrongCodeReturnsNil(t *testing.T) {
	xoluErr := &TypedError{Code: PrefixXolu + string(CodeNotFound), Message: "not found"}
	if WrapXoluInvalidTransition(xoluErr) != nil {
		t.Error("expected nil for wrong code")
	}
}

func TestWrapXoluError_Passthrough(t *testing.T) {
	xoluErr := &TypedError{Code: PrefixXolu + "CUSTOM_CODE", Message: "custom message", Details: map[string]any{"foo": "bar"}}
	wrapped := WrapXoluError(xoluErr)
	requireNotNil(t, wrapped)
	requireCode(t, wrapped, PrefixXolu+"CUSTOM_CODE")
	requireMessageContains(t, wrapped, "custom message")
	if wrapped.Details["foo"] != "bar" {
		t.Errorf("details not preserved: %v", wrapped.Details)
	}
}

func TestWrapXoluError_NilReturnsNil(t *testing.T) {
	if WrapXoluError(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestNewNotOffered_JSON(t *testing.T) {
	err := NewNotOffered("tenant-123", "work_orders.CreateOrder")
	data, _ := json.Marshal(err)
	var decoded TypedError
	json.Unmarshal(data, &decoded)
	requireCode(t, &decoded, PrefixMolu+string(CodeNotOffered))
	if decoded.Details["tenant"] != "tenant-123" || decoded.Details["tool"] != "work_orders.CreateOrder" {
		t.Errorf("details not preserved: %v", decoded.Details)
	}
}

func TestNewNotFound_JSON(t *testing.T) {
	err := NewNotFound("tenant-123", "order-456")
	data, _ := json.Marshal(err)
	var decoded TypedError
	json.Unmarshal(data, &decoded)
	requireCode(t, &decoded, PrefixMolu+string(CodeNotFound))
	if decoded.Details["tenant"] != "tenant-123" || decoded.Details["objectId"] != "order-456" {
		t.Errorf("details not preserved: %v", decoded.Details)
	}
}

func TestNewEmpty_JSON(t *testing.T) {
	err := NewEmpty("tenant-123", "find", map[string]any{"status": "open"})
	data, _ := json.Marshal(err)
	var decoded TypedError
	json.Unmarshal(data, &decoded)
	requireCode(t, &decoded, PrefixMolu+string(CodeEmpty))
	if decoded.Details["tenant"] != "tenant-123" || decoded.Details["operation"] != "find" {
		t.Errorf("details not preserved: %v", decoded.Details)
	}
}

func TestNewContractViolation_JSON(t *testing.T) {
	err := NewContractViolation("tenant-123", "work_orders.CreateOrder", []string{"title", "assignee"})
	data, _ := json.Marshal(err)
	var decoded TypedError
	json.Unmarshal(data, &decoded)
	requireCode(t, &decoded, PrefixMolu+string(CodeContractViolation))
	fieldsIfc, ok := decoded.Details["fields"].([]any)
	if !ok {
		t.Errorf("fields not []any: %v", decoded.Details["fields"])
	}
	fields := make([]string, len(fieldsIfc))
	for i, v := range fieldsIfc {
		fields[i] = v.(string)
	}
	if len(fields) != 2 || fields[0] != "title" || fields[1] != "assignee" {
		t.Errorf("fields not preserved: %v", fields)
	}
}

func TestNewSubstrateUnavailable_JSON(t *testing.T) {
	err := NewSubstrateUnavailable("2026-01-01T10:00:00Z", "2026-01-01T10:30:00Z", 5, 10)
	data, _ := json.Marshal(err)
	var decoded TypedError
	json.Unmarshal(data, &decoded)
	requireCode(t, &decoded, PrefixMolu+string(CodeSubstrateUnavailable))
	if decoded.Details["lastSuccessfulContact"] != "2026-01-01T10:00:00Z" ||
		decoded.Details["nextRetryAt"] != "2026-01-01T10:30:00Z" {
		t.Errorf("retry metadata strings not preserved: %v", decoded.Details)
	}
	attempt := int(decoded.Details["attemptNumber"].(float64))
	max := int(decoded.Details["maxAttempts"].(float64))
	if attempt != 5 || max != 10 {
		t.Errorf("retry metadata ints not preserved: attempt=%d max=%d", attempt, max)
	}
}

func TestWrapXoluNotFound_JSON(t *testing.T) {
	xoluErr := &TypedError{Code: PrefixXolu + string(CodeNotFound), Message: "xolu not found"}
	wrapped := WrapXoluNotFound(xoluErr)
	data, _ := json.Marshal(wrapped)
	var decoded TypedError
	json.Unmarshal(data, &decoded)
	requireCode(t, &decoded, PrefixXolu+string(CodeNotFound))
	requireMessageContains(t, &decoded, "xolu not found")
}

func TestWrapXoluInvalidTransition_JSON(t *testing.T) {
	xoluErr := &TypedError{Code: PrefixXolu + string(CodeInvalidTransition), Message: "guard: invalid state"}
	wrapped := WrapXoluInvalidTransition(xoluErr)
	data, _ := json.Marshal(wrapped)
	var decoded TypedError
	json.Unmarshal(data, &decoded)
	requireCode(t, &decoded, PrefixXolu+string(CodeInvalidTransition))
	requireMessageContains(t, &decoded, "guard: invalid state")
}

func TestWrapXoluError_CustomCode_JSON(t *testing.T) {
	xoluErr := &TypedError{Code: PrefixXolu + "CUSTOM_CODE", Message: "custom"}
	wrapped := WrapXoluError(xoluErr)
	data, _ := json.Marshal(wrapped)
	var decoded TypedError
	json.Unmarshal(data, &decoded)
	requireCode(t, &decoded, PrefixXolu+"CUSTOM_CODE")
	requireMessageContains(t, &decoded, "custom")
}