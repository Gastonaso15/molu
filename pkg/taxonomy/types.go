package taxonomy

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

func (e *TypedError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Code + ": " + e.Message
}