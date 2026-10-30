package bridge_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gastonaso15/molu/pkg/bridge"
)

func TestNormalizeTokenRef(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"vault:token-1", "MOLU_FRONT_TOKEN_VAULT_TOKEN_1"},
		{"app.secret", "MOLU_FRONT_TOKEN_APP_SECRET"},
		{"apiKey", "MOLU_FRONT_TOKEN_APIKEY"},
		{"my-token_123", "MOLU_FRONT_TOKEN_MY_TOKEN_123"},
		{"crm/auth/v1", "MOLU_FRONT_TOKEN_CRM_AUTH_V1"},
		{"", "MOLU_FRONT_TOKEN_"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := bridge.NormalizeTokenRef(tc.input)
			if got != tc.expected {
				t.Fatalf("NormalizeTokenRef(%q) = %q, expected %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestEnvSecretResolver_DirectEnv(t *testing.T) {
	resolver := bridge.NewEnvSecretResolver()

	tokenRef := "service-a:api-key"
	envVar := bridge.NormalizeTokenRef(tokenRef)
	secretValue := "super-secret-key-42"

	t.Setenv(envVar, secretValue)

	got, err := resolver.Resolve(tokenRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != secretValue {
		t.Fatalf("expected secret %q, got %q", secretValue, got)
	}
}

func TestEnvSecretResolver_FromFile(t *testing.T) {
	resolver := bridge.NewEnvSecretResolver()

	tokenRef := "vault:token-file"
	envVarFile := bridge.NormalizeTokenRef(tokenRef) + "_FILE"

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "secret.txt")
	secretContent := "file-secret-payload-99"
	if err := os.WriteFile(filePath, []byte(secretContent+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	t.Setenv(envVarFile, filePath)

	got, err := resolver.Resolve(tokenRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != secretContent {
		t.Fatalf("expected secret %q, got %q", secretContent, got)
	}
}

func TestEnvSecretResolver_FilePrecedenceOverEnv(t *testing.T) {
	resolver := bridge.NewEnvSecretResolver()

	tokenRef := "oauth:client"
	envVar := bridge.NormalizeTokenRef(tokenRef)
	envVarFile := envVar + "_FILE"

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "secret.txt")
	fileSecret := "from-file-priority"
	if err := os.WriteFile(filePath, []byte(fileSecret), 0o600); err != nil {
		t.Fatalf("failed to write secret file: %v", err)
	}

	t.Setenv(envVar, "from-env-direct")
	t.Setenv(envVarFile, filePath)

	got, err := resolver.Resolve(tokenRef)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != fileSecret {
		t.Fatalf("expected file to take precedence, got %q", got)
	}
}

func TestEnvSecretResolver_NotFound(t *testing.T) {
	resolver := bridge.NewEnvSecretResolver()

	got, err := resolver.Resolve("non_existent_token_ref")
	if err == nil {
		t.Fatalf("expected error for non existent secret, got secret %q", got)
	}
	if !errors.Is(err, bridge.ErrSecretNotFound) {
		t.Fatalf("expected ErrSecretNotFound, got %v", err)
	}
}

func TestEnvSecretResolver_FileReadError(t *testing.T) {
	resolver := bridge.NewEnvSecretResolver()

	tokenRef := "broken:file"
	envVarFile := bridge.NormalizeTokenRef(tokenRef) + "_FILE"
	t.Setenv(envVarFile, "/path/to/non/existent/secret/file.txt")

	_, err := resolver.Resolve(tokenRef)
	if err == nil {
		t.Fatalf("expected error when secret file does not exist")
	}
}

func TestBridge_DomainNeutrality(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("failed to glob files: %v", err)
	}

	forbidden := string([]byte{'s', 'e', 'a', 'm'})

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("failed to read %s: %v", file, err)
		}
		if strings.Contains(strings.ToLower(string(content)), forbidden) {
			t.Fatalf("file %s violates domain neutrality by mentioning forbidden word", file)
		}
	}
}
