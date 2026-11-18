package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mapLookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		val, ok := env[key]
		return val, ok
	}
}

func TestLoadFromLookup_ValidConfiguration(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"APP_ENV":                 "production",
		"APP_PORT":                "9090",
		"DATABASE_URL":            "postgresql://postgres.ref:supersecret@aws-0-ap-southeast-1.pooler.supabase.com:5432/postgres?sslmode=require",
		"FRONTEND_ORIGIN":         "https://app.example.com",
		"HF_TOKEN":                "hf_topsecret_token_12345",
		"HF_MODEL_ID":             "Qwen/Qwen2.5-72B-Instruct",
		"HF_BASE_URL":             "https://router.huggingface.co/v1",
		"HF_PROVIDER":             "cheapest",
		"AI_MAX_OUTPUT_TOKENS":    "1024",
		"AI_TEMPERATURE":          "0.8",
		"AI_TIMEOUT":              "30s",
		"AI_MAX_CONTEXT_MESSAGES": "15",
		"AI_SYSTEM_PROMPT":        "Custom system prompt",
	}

	cfg, err := LoadFromLookup(mapLookup(env))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.AppEnv != "production" {
		t.Errorf("expected AppEnv %q, got %q", "production", cfg.AppEnv)
	}
	if cfg.AppPort != 9090 {
		t.Errorf("expected AppPort 9090, got %d", cfg.AppPort)
	}
	if cfg.FrontendOrigin != "https://app.example.com" {
		t.Errorf("expected FrontendOrigin %q, got %q", "https://app.example.com", cfg.FrontendOrigin)
	}
	if cfg.Address() != "0.0.0.0:9090" {
		t.Errorf("expected Address %q, got %q", "0.0.0.0:9090", cfg.Address())
	}
	if cfg.HFToken != "hf_topsecret_token_12345" {
		t.Errorf("expected HFToken %q, got %q", "hf_topsecret_token_12345", cfg.HFToken)
	}
	if cfg.HFModelID != "Qwen/Qwen2.5-72B-Instruct" {
		t.Errorf("expected HFModelID %q, got %q", "Qwen/Qwen2.5-72B-Instruct", cfg.HFModelID)
	}
	if cfg.AIMaxOutputTokens != 1024 {
		t.Errorf("expected AIMaxOutputTokens 1024, got %d", cfg.AIMaxOutputTokens)
	}
	if cfg.AITemperature != 0.8 {
		t.Errorf("expected AITemperature 0.8, got %f", cfg.AITemperature)
	}
	if cfg.AITimeout != 30*time.Second {
		t.Errorf("expected AITimeout 30s, got %v", cfg.AITimeout)
	}
	if cfg.AIMaxContextMessages != 15 {
		t.Errorf("expected AIMaxContextMessages 15, got %d", cfg.AIMaxContextMessages)
	}
	if cfg.AISystemPrompt != "Custom system prompt" {
		t.Errorf("expected AISystemPrompt %q, got %q", "Custom system prompt", cfg.AISystemPrompt)
	}
	if strings.Contains(cfg.String(), "supersecret") {
		t.Errorf("expected Config.String() to redact DATABASE_URL, got %q", cfg.String())
	}
	if strings.Contains(cfg.String(), "hf_topsecret") {
		t.Errorf("expected Config.String() to redact HF_TOKEN, got %q", cfg.String())
	}
}

func TestLoadFromLookup_Defaults(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost:5432/chatbot?sslmode=disable",
	}

	cfg, err := LoadFromLookup(mapLookup(env))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.AppEnv != DefaultAppEnv {
		t.Errorf("expected default AppEnv %q, got %q", DefaultAppEnv, cfg.AppEnv)
	}
	if cfg.AppPort != DefaultAppPort {
		t.Errorf("expected default AppPort %d, got %d", DefaultAppPort, cfg.AppPort)
	}
	if cfg.FrontendOrigin != DefaultFrontendOrigin {
		t.Errorf("expected default FrontendOrigin %q, got %q", DefaultFrontendOrigin, cfg.FrontendOrigin)
	}
	if cfg.DBMaxConns != DefaultDBMaxConns {
		t.Errorf("expected default DBMaxConns %d, got %d", DefaultDBMaxConns, cfg.DBMaxConns)
	}
	if cfg.DBMinConns != DefaultDBMinConns {
		t.Errorf("expected default DBMinConns %d, got %d", DefaultDBMinConns, cfg.DBMinConns)
	}
}

func TestLoadFromLookup_MissingDatabaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "unset",
			env:  map[string]string{},
		},
		{
			name: "empty",
			env:  map[string]string{"DATABASE_URL": "   "},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFromLookup(mapLookup(tc.env))
			if !errors.Is(err, ErrMissingDatabaseURL) {
				t.Fatalf("expected ErrMissingDatabaseURL, got %v", err)
			}
		})
	}
}

func TestLoadFromLookup_InvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "mysql scheme",
			url:  "mysql://user:secretpass@localhost:3306/db",
		},
		{
			name: "http scheme",
			url:  "http://user:secretpass@localhost:5432/db",
		},
		{
			name: "missing scheme",
			url:  "localhost:5432/db",
		},
		{
			name: "missing host",
			url:  "postgres:///dbname",
		},
		{
			name: "invalid port",
			url:  "postgres://user:secretpass@localhost:99999/db",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{"DATABASE_URL": tc.url}
			_, err := LoadFromLookup(mapLookup(env))
			if !errors.Is(err, ErrInvalidDatabaseURL) {
				t.Fatalf("expected ErrInvalidDatabaseURL for %q, got %v", tc.name, err)
			}
			if strings.Contains(err.Error(), "secretpass") {
				t.Fatalf("error leaked secret password: %v", err)
			}
		})
	}
}

func TestLoadFromLookup_InvalidPort(t *testing.T) {
	t.Parallel()

	tests := []string{"0", "-1", "65536", "abc", "8080a"}

	for _, port := range tests {
		t.Run(port, func(t *testing.T) {
			t.Parallel()
			env := map[string]string{
				"APP_PORT":     port,
				"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
			}
			_, err := LoadFromLookup(mapLookup(env))
			if !errors.Is(err, ErrInvalidAppPort) {
				t.Fatalf("expected ErrInvalidAppPort for port %q, got %v", port, err)
			}
		})
	}
}

func TestLoad_OptionalEnvFile(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("APP_PORT", "8081")

	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.env"))
	if err != nil {
		t.Fatalf("expected missing .env file to be ignored when env vars exist, got %v", err)
	}
	if cfg.AppPort != 8081 {
		t.Errorf("expected AppPort 8081, got %d", cfg.AppPort)
	}

	envFile := filepath.Join(t.TempDir(), ".env")
	content := "APP_ENV=staging\nFRONTEND_ORIGIN=http://localhost:3000\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write temp .env: %v", err)
	}

	cfgFromFile, err := Load(envFile)
	if err != nil {
		t.Fatalf("expected Load with .env file to succeed, got %v", err)
	}
	if cfgFromFile.AppEnv != "staging" {
		t.Errorf("expected AppEnv %q from .env file, got %q", "staging", cfgFromFile.AppEnv)
	}
}

func TestLoadFromLookup_DBPoolClamping(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
		"DB_MAX_CONNS": "1",
	}

	cfg, err := LoadFromLookup(mapLookup(env))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.DBMaxConns != 1 || cfg.DBMinConns != 1 {
		t.Errorf("expected DBMaxConns=1 and DBMinConns=1, got max=%d min=%d", cfg.DBMaxConns, cfg.DBMinConns)
	}
}

func TestLoadFromLookup_AIDefaults(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
	}

	cfg, err := LoadFromLookup(mapLookup(env))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.HFToken != "" {
		t.Errorf("expected empty HFToken, got %q", cfg.HFToken)
	}
	if cfg.HFModelID != DefaultHFModelID {
		t.Errorf("expected default HFModelID %q, got %q", DefaultHFModelID, cfg.HFModelID)
	}
	if cfg.HFBaseURL != DefaultHFBaseURL {
		t.Errorf("expected default HFBaseURL %q, got %q", DefaultHFBaseURL, cfg.HFBaseURL)
	}
	if cfg.AIMaxOutputTokens != DefaultAIMaxOutputTokens {
		t.Errorf("expected default AIMaxOutputTokens %d, got %d", DefaultAIMaxOutputTokens, cfg.AIMaxOutputTokens)
	}
	if cfg.AITemperature != DefaultAITemperature {
		t.Errorf("expected default AITemperature %f, got %f", DefaultAITemperature, cfg.AITemperature)
	}
	if cfg.AITimeout != DefaultAITimeout {
		t.Errorf("expected default AITimeout %v, got %v", DefaultAITimeout, cfg.AITimeout)
	}
	if cfg.AIMaxContextMessages != DefaultAIMaxContextMsgs {
		t.Errorf("expected default AIMaxContextMessages %d, got %d", DefaultAIMaxContextMsgs, cfg.AIMaxContextMessages)
	}
	if cfg.AISystemPrompt != DefaultAISystemPrompt {
		t.Errorf("expected default AISystemPrompt %q, got %q", DefaultAISystemPrompt, cfg.AISystemPrompt)
	}
}

func TestLoadFromLookup_InvalidAISettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
	}{
		{"invalid base url", map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/db", "HF_BASE_URL": "ftp://bad"}},
		{"negative max tokens", map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/db", "AI_MAX_OUTPUT_TOKENS": "-5"}},
		{"too high max tokens", map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/db", "AI_MAX_OUTPUT_TOKENS": "9000"}},
		{"negative temp", map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/db", "AI_TEMPERATURE": "-0.1"}},
		{"temp above 2.0", map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/db", "AI_TEMPERATURE": "2.5"}},
		{"invalid timeout", map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/db", "AI_TIMEOUT": "invalid"}},
		{"zero context messages", map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/db", "AI_MAX_CONTEXT_MESSAGES": "0"}},
		{"too high context messages", map[string]string{"DATABASE_URL": "postgres://user:pass@localhost:5432/db", "AI_MAX_CONTEXT_MESSAGES": "101"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := LoadFromLookup(mapLookup(tc.env))
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestLoadFromLookup_GeminiAndProviderSelection(t *testing.T) {
	t.Parallel()

	t.Run("explicit gemini provider with key and model", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"DATABASE_URL":    "postgres://user:pass@localhost:5432/db",
			"AI_PROVIDER":     "gemini",
			"GEMINI_API_KEY":  "secret-gemini-key-12345",
			"GEMINI_MODEL_ID": "gemini-3.5-flash",
		}
		cfg, err := LoadFromLookup(mapLookup(env))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AIProvider != "gemini" {
			t.Errorf("expected AIProvider gemini, got %q", cfg.AIProvider)
		}
		if cfg.GeminiModelID != "gemini-3.5-flash" {
			t.Errorf("expected GeminiModelID gemini-3.5-flash, got %q", cfg.GeminiModelID)
		}
		if strings.Contains(cfg.String(), "secret-gemini-key") {
			t.Errorf("expected GeminiAPIKey to be redacted, got %s", cfg.String())
		}
	})

	t.Run("fallback to GEMINI_MODEL", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
			"GEMINI_MODEL": "gemini-3.8-flash",
		}
		cfg, err := LoadFromLookup(mapLookup(env))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.GeminiModelID != "gemini-3.8-flash" {
			t.Errorf("expected fallback GeminiModelID gemini-3.8-flash, got %q", cfg.GeminiModelID)
		}
	})

	t.Run("explicit huggingface provider", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
			"AI_PROVIDER":  "huggingface",
			"HF_TOKEN":     "hf-token-test",
		}
		cfg, err := LoadFromLookup(mapLookup(env))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AIProvider != "huggingface" {
			t.Errorf("expected AIProvider huggingface, got %q", cfg.AIProvider)
		}
	})

	t.Run("invalid provider rejected", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
			"AI_PROVIDER":  "openai",
		}
		_, err := LoadFromLookup(mapLookup(env))
		if !errors.Is(err, ErrInvalidAIProvider) {
			t.Errorf("expected ErrInvalidAIProvider, got %v", err)
		}
	})
}

func TestLoadFromLookup_PortPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("PORT takes precedence over APP_PORT", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
			"PORT":         "10000",
			"APP_PORT":     "9090",
		}
		cfg, err := LoadFromLookup(mapLookup(env))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AppPort != 10000 {
			t.Errorf("expected AppPort 10000, got %d", cfg.AppPort)
		}
		if cfg.Address() != "0.0.0.0:10000" {
			t.Errorf("expected Address %q, got %q", "0.0.0.0:10000", cfg.Address())
		}
	})

	t.Run("falls back to APP_PORT when PORT is empty", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
			"PORT":         "   ",
			"APP_PORT":     "9091",
		}
		cfg, err := LoadFromLookup(mapLookup(env))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AppPort != 9091 {
			t.Errorf("expected AppPort 9091, got %d", cfg.AppPort)
		}
		if cfg.Address() != "0.0.0.0:9091" {
			t.Errorf("expected Address %q, got %q", "0.0.0.0:9091", cfg.Address())
		}
	})

	t.Run("defaults to 8080 when neither PORT nor APP_PORT is set", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
		}
		cfg, err := LoadFromLookup(mapLookup(env))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.AppPort != DefaultAppPort {
			t.Errorf("expected AppPort %d, got %d", DefaultAppPort, cfg.AppPort)
		}
		if cfg.Address() != "0.0.0.0:8080" {
			t.Errorf("expected Address %q, got %q", "0.0.0.0:8080", cfg.Address())
		}
	})

	t.Run("invalid PORT fails even if APP_PORT is valid", func(t *testing.T) {
		t.Parallel()
		env := map[string]string{
			"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
			"PORT":         "invalid",
			"APP_PORT":     "8080",
		}
		_, err := LoadFromLookup(mapLookup(env))
		if !errors.Is(err, ErrInvalidAppPort) {
			t.Fatalf("expected ErrInvalidAppPort, got %v", err)
		}
	})
}
