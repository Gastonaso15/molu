package bridge

import (
	"fmt"
	"os"
	"strings"
	"unicode"
)

const envTokenPrefix = "MOLU_FRONT_TOKEN_"

// NormalizeTokenRef transforms an arbitrary token reference string into a valid
// environment variable name prefixed with MOLU_FRONT_TOKEN_.
// Non-alphanumeric characters are converted to underscores and letters are uppercased.
func NormalizeTokenRef(tokenRef string) string {
	var sb strings.Builder
	sb.WriteString(envTokenPrefix)

	for _, r := range tokenRef {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToUpper(r))
		} else {
			sb.WriteRune('_')
		}
	}

	return sb.String()
}

// EnvSecretResolver resolves token references using environment variables,
// prioritizing file-based secrets (_FILE suffix).
type EnvSecretResolver struct{}

// NewEnvSecretResolver returns a new instance of EnvSecretResolver.
func NewEnvSecretResolver() *EnvSecretResolver {
	return &EnvSecretResolver{}
}

// Resolve looks up the secret for the given tokenRef.
// It first checks if <ENV>_FILE is defined, reading the file contents.
// If not found, it checks <ENV> directly.
// If neither exists, it returns ErrSecretNotFound.
func (r *EnvSecretResolver) Resolve(tokenRef string) (string, error) {
	envVar := NormalizeTokenRef(tokenRef)
	fileEnv := envVar + "_FILE"

	if filePath, ok := os.LookupEnv(fileEnv); ok && filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read secret file %q: %w", filePath, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	if val, ok := os.LookupEnv(envVar); ok {
		return val, nil
	}

	return "", fmt.Errorf("%w: %q (expected %s or %s)", ErrSecretNotFound, tokenRef, envVar, fileEnv)
}
