package taxonomy

// ErrorCode represents a typed error code from the taxonomy.
type ErrorCode string

const (
	// CodeOK indicates successful operation.
	CodeOK ErrorCode = "OK"
	// CodeNotOffered indicates the function/entity exists but not for this tenant/role.
	CodeNotOffered ErrorCode = "NOT_OFFERED"
	// CodeNotFound indicates the addressed object doesn't exist for this tenant.
	CodeNotFound ErrorCode = "NOT_FOUND"
	// CodeEmpty indicates a valid query returned no results.
	CodeEmpty ErrorCode = "EMPTY"
	// CodeInvalidTransition indicates an FSM transition was rejected by xolu.
	CodeInvalidTransition ErrorCode = "INVALID_TRANSITION"
	// CodeContractViolation indicates input violates the publisher's JSON schema.
	CodeContractViolation ErrorCode = "CONTRACT_VIOLATION"
	// CodeSubstrateUnavailable indicates xolu is unreachable.
	CodeSubstrateUnavailable ErrorCode = "SUBSTRATE_UNAVAILABLE"
)

// Prefixes for error codes.
const (
	PrefixMolu = "XOLU-MOLU-FRONT-"
	PrefixXolu = "XOLU-"
)

// FullCode returns the full prefixed code for a taxonomy error.
func FullCode(prefix string, code ErrorCode) string {
	return prefix + string(code)
}

// IsXoluCode checks if a code string has the XOLU- prefix.
func IsXoluCode(code string) bool {
	return len(code) >= len(PrefixXolu) && code[:len(PrefixXolu)] == PrefixXolu
}

// IsMoluCode checks if a code string has the XOLU-MOLU-FRONT- prefix.
func IsMoluCode(code string) bool {
	return len(code) >= len(PrefixMolu) && code[:len(PrefixMolu)] == PrefixMolu
}

// StripPrefix removes the prefix from a code, returning the base ErrorCode.
func StripPrefix(code string) ErrorCode {
	if IsMoluCode(code) {
		return ErrorCode(code[len(PrefixMolu):])
	}
	if IsXoluCode(code) {
		return ErrorCode(code[len(PrefixXolu):])
	}
	return ErrorCode(code)
}