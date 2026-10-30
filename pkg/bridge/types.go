package bridge

import (
	"context"
	"errors"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/rpi"
)

// ErrSecretNotFound is returned when a secret reference cannot be resolved.
var ErrSecretNotFound = errors.New("secret not found")

// AuthMode represents the authentication mechanism for a domain function.
type AuthMode string

const (
	AuthModeNone   AuthMode = "none"
	AuthModeBearer AuthMode = "bearer"
	AuthModeAPIKey AuthMode = "apikey"
)

// AuthMetadata specifies the authentication requirement declared in a contract.
type AuthMetadata struct {
	Mode     AuthMode `json:"mode"`
	Header   string   `json:"header"`
	TokenRef string   `json:"token_ref"`
}

// ToolLookup retrieves a tool registration from the catalogue by its qualified name.
type ToolLookup interface {
	Lookup(name string) (hub.ToolRegistration, bool)
}

// SecretResolver resolves a token reference into an actual secret string.
type SecretResolver interface {
	Resolve(tokenRef string) (string, error)
}

// DomainCaller executes a direct HTTP invocation against a published domain function endpoint.
type DomainCaller interface {
	Call(ctx context.Context, endpoint string, auth AuthMetadata, args map[string]any) (any, error)
}

// DomainExecutor orchestrates admission gating, validation, and execution of domain functions.
type DomainExecutor interface {
	Execute(ctx context.Context, toolName string, args map[string]any, actx rpi.AdmitContext) (any, error)
}
