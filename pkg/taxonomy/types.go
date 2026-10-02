package taxonomy

type ErrorCode string

const (
	CodeNotOffered            ErrorCode = "NOT_OFFERED"
	CodeNotFound              ErrorCode = "NOT_FOUND"
	CodeEmpty                 ErrorCode = "EMPTY"
	CodeInvalidTransition     ErrorCode = "INVALID_TRANSITION"
	CodeContractViolation     ErrorCode = "CONTRACT_VIOLATION"
	CodeSubstrateUnavailable  ErrorCode = "SUBSTRATE_UNAVAILABLE"
)

type TypedError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type RetryMetadata struct {
	LastSuccessfulContact string `json:"lastSuccessfulContact"`
	NextRetryAt           string `json:"nextRetryAt"`
	AttemptNumber         int    `json:"attemptNumber"`
	MaxAttempts           int    `json:"maxAttempts"`
}

const (
	PrefixMolu = "XOLU-MOLU-FRONT-"
	PrefixXolu = "XOLU-"
)

func (e *TypedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Code + ": " + e.Message
}