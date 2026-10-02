package taxonomy

import (
	"fmt"
	"time"
)

func NewNotOffered(tenant, tool string) *TypedError {
	return &TypedError{
		Code:    PrefixMolu + string(CodeNotOffered),
		Message: fmt.Sprintf("tool %q not offered for tenant %q", tool, tenant),
		Details: map[string]any{"tenant": tenant, "tool": tool},
	}
}

func NewNotFound(tenant, objectID string) *TypedError {
	return &TypedError{
		Code:    PrefixMolu + string(CodeNotFound),
		Message: fmt.Sprintf("object %q not found for tenant %q", objectID, tenant),
		Details: map[string]any{"tenant": tenant, "objectId": objectID},
	}
}

func NewEmpty(tenant, operation string, query map[string]any) *TypedError {
	details := map[string]any{"tenant": tenant, "operation": operation}
	for k, v := range query {
		details["query."+k] = v
	}
	return &TypedError{
		Code:    PrefixMolu + string(CodeEmpty),
		Message: fmt.Sprintf("query %q returned no results for tenant %q", operation, tenant),
		Details: details,
	}
}

func NewContractViolation(tenant, tool string, invalidFields []string) *TypedError {
	return &TypedError{
		Code:    PrefixMolu + string(CodeContractViolation),
		Message: fmt.Sprintf("contract violation for tool %q (tenant %q): invalid fields %v", tool, tenant, invalidFields),
		Details: map[string]any{"tenant": tenant, "tool": tool, "fields": invalidFields},
	}
}

func NewSubstrateUnavailable(lastContact, nextRetry string, attempt, maxAttempts int) *TypedError {
	return &TypedError{
		Code:    PrefixMolu + string(CodeSubstrateUnavailable),
		Message: fmt.Sprintf("substrate xolu unavailable (attempt %d/%d, last contact: %s, next retry: %s)", attempt, maxAttempts, lastContact, nextRetry),
		Details: map[string]any{
			"lastSuccessfulContact": lastContact,
			"nextRetryAt":           nextRetry,
			"attemptNumber":         attempt,
			"maxAttempts":           maxAttempts,
		},
	}
}

func WrapXoluNotFound(xoluErr *TypedError) *TypedError {
	if xoluErr == nil || xoluErr.Code != PrefixXolu+string(CodeNotFound) {
		return nil
	}
	return xoluErr
}

func WrapXoluInvalidTransition(xoluErr *TypedError) *TypedError {
	if xoluErr == nil || xoluErr.Code != PrefixXolu+string(CodeInvalidTransition) {
		return nil
	}
	return xoluErr
}

func WrapXoluError(xoluErr *TypedError) *TypedError {
	if xoluErr == nil {
		return nil
	}
	return xoluErr
}

func NowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func NextRetryRFC3339(attempt int, floor, ceiling time.Duration) string {
	backoff := floor
	for i := 1; i < attempt; i++ {
		backoff *= 2
		if backoff > ceiling {
			backoff = ceiling
			break
		}
	}
	return time.Now().UTC().Add(backoff).Format(time.RFC3339)
}