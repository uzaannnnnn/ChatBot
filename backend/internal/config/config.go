package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	DefaultAppEnv              = "development"
	DefaultAppPort             = 8080
	DefaultFrontendOrigin      = "http://localhost:5173"
	DefaultDBMaxConns          = int32(10)
	DefaultDBMinConns          = int32(2)
	DefaultDBMaxConnLifetime   = 30 * time.Minute
	DefaultDBMaxConnIdleTime   = 5 * time.Minute
	DefaultDBConnectTimeout    = 5 * time.Second
	DefaultShutdownTimeout     = 10 * time.Second
	DefaultReadinessTimeout    = 2 * time.Second
	DefaultMaxRequestBodyBytes = int64(1 << 20) // 1 MiB

	DefaultAIProvider        = "gemini"
	DefaultGeminiBaseURL     = "https://generativelanguage.googleapis.com/v1beta"
	DefaultGeminiModelID     = "gemini-3.1-flash-lite"
	DefaultHFBaseURL         = "https://router.huggingface.co/v1"
	DefaultHFModelID         = "meta-llama/Llama-3.3-70B-Instruct"
	DefaultAIMaxOutputTokens = 2048
	DefaultAITemperature     = 0.7
	DefaultAITimeout         = 60 * time.Second
	DefaultAIMaxContextMsgs  = 20
	DefaultAISystemPrompt    = "You are a helpful, respectful, and honest AI assistant."
)

var (
	ErrMissingDatabaseURL   = errors.New("config: DATABASE_URL is required")
	ErrInvalidDatabaseURL   = errors.New("config: DATABASE_URL must be a valid postgres or postgresql URL with a host")
	ErrInvalidAppPort       = errors.New("config: APP_PORT must be an integer between 1 and 65535")
	ErrInvalidOrigin        = errors.New("config: FRONTEND_ORIGIN must be a valid http or https origin without wildcard")
	ErrInvalidHFBaseURL     = errors.New("config: HF_BASE_URL must be a valid http or https URL")
	ErrInvalidGeminiBaseURL = errors.New("config: GEMINI_BASE_URL must be a valid http or https URL")
	ErrInvalidAIProvider    = errors.New("config: AI_PROVIDER must be 'gemini' or 'huggingface'")
)

// Config holds the runtime configuration for the backend service.
type Config struct {
	AppEnv              string
	AppPort             int
	DatabaseURL         string
	FrontendOrigin      string
	DBMaxConns          int32
	DBMinConns          int32
	DBMaxConnLifetime   time.Duration
	DBMaxConnIdleTime   time.Duration
	DBConnectTimeout    time.Duration
	ShutdownTimeout     time.Duration
	ReadinessTimeout    time.Duration
	MaxRequestBodyBytes int64

	// Supabase Auth configuration
	SupabaseURL       string
	SupabaseJWKSURL   string
	SupabaseJWTSecret string

	// AI Provider configuration
	AIProvider    string
	GeminiAPIKey  string
	GeminiModelID string
	GeminiBaseURL string

	// Hugging Face configuration
	HFToken              string
	HFModelID            string
	HFBaseURL            string
	HFProvider           string
	AIMaxOutputTokens    int
	AITemperature        float64
	AITimeout            time.Duration
	AIMaxContextMessages int
	AISystemPrompt       string
}

// Load reads configuration from optional .env files and the process environment.
// Missing .env files are ignored so production environments can supply variables directly.
func Load(envPaths ...string) (*Config, error) {
	if len(envPaths) == 0 {
		_ = godotenv.Load(".env")
	} else {
		for _, p := range envPaths {
			if p != "" {
				_ = godotenv.Load(p)
			}
		}
	}

	return LoadFromLookup(os.LookupEnv)
}

// LoadFromLookup builds and validates a Config using the provided lookup function.
// This allows deterministic unit tests without mutating global process state.
func LoadFromLookup(lookup func(string) (string, bool)) (*Config, error) {
	cfg := &Config{
		AppEnv:               getString(lookup, "APP_ENV", DefaultAppEnv),
		FrontendOrigin:       getString(lookup, "FRONTEND_ORIGIN", DefaultFrontendOrigin),
		DBMaxConns:           DefaultDBMaxConns,
		DBMinConns:           DefaultDBMinConns,
		DBMaxConnLifetime:    DefaultDBMaxConnLifetime,
		DBMaxConnIdleTime:    DefaultDBMaxConnIdleTime,
		DBConnectTimeout:     DefaultDBConnectTimeout,
		ShutdownTimeout:      DefaultShutdownTimeout,
		ReadinessTimeout:     DefaultReadinessTimeout,
		MaxRequestBodyBytes:  DefaultMaxRequestBodyBytes,
		HFToken:              getString(lookup, "HF_TOKEN", ""),
		HFModelID:            getString(lookup, "HF_MODEL_ID", DefaultHFModelID),
		HFBaseURL:            getString(lookup, "HF_BASE_URL", DefaultHFBaseURL),
		HFProvider:           getString(lookup, "HF_PROVIDER", ""),
		AIMaxOutputTokens:    DefaultAIMaxOutputTokens,
		AITemperature:        DefaultAITemperature,
		AITimeout:            DefaultAITimeout,
		AIMaxContextMessages: DefaultAIMaxContextMsgs,
		AISystemPrompt:       getString(lookup, "AI_SYSTEM_PROMPT", DefaultAISystemPrompt),
	}

	portStr := getString(lookup, "APP_PORT", strconv.Itoa(DefaultAppPort))
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return nil, ErrInvalidAppPort
	}
	cfg.AppPort = port

	dbURL, ok := lookup("DATABASE_URL")
	dbURL = strings.TrimSpace(dbURL)
	if !ok || dbURL == "" {
		return nil, ErrMissingDatabaseURL
	}
	if err := validateDatabaseURL(dbURL); err != nil {
		return nil, err
	}
	cfg.DatabaseURL = dbURL

	if err := validateFrontendOrigin(cfg.FrontendOrigin); err != nil {
		return nil, err
	}

	// Provider selection
	aiProvider := strings.ToLower(getString(lookup, "AI_PROVIDER", ""))
	if aiProvider == "" {
		if _, ok := lookup("GEMINI_API_KEY"); ok {
			aiProvider = "gemini"
		} else if _, ok := lookup("HF_TOKEN"); ok {
			aiProvider = "huggingface"
		} else {
			aiProvider = DefaultAIProvider
		}
	}
	if aiProvider != "gemini" && aiProvider != "huggingface" {
		return nil, ErrInvalidAIProvider
	}
	cfg.AIProvider = aiProvider

	// Gemini configuration
	geminiModel := getString(lookup, "GEMINI_MODEL_ID", "")
	if geminiModel == "" {
		geminiModel = getString(lookup, "GEMINI_MODEL", DefaultGeminiModelID)
	}
	cfg.GeminiModelID = geminiModel
	cfg.GeminiAPIKey = getString(lookup, "GEMINI_API_KEY", "")
	cfg.GeminiBaseURL = getString(lookup, "GEMINI_BASE_URL", DefaultGeminiBaseURL)

	if err := validateURL(cfg.GeminiBaseURL); err != nil {
		return nil, ErrInvalidGeminiBaseURL
	}

	if err := validateURL(cfg.HFBaseURL); err != nil {
		return nil, ErrInvalidHFBaseURL
	}

	cfg.SupabaseURL = getString(lookup, "SUPABASE_URL", "")
	if cfg.SupabaseURL != "" {
		if err := validateURL(cfg.SupabaseURL); err != nil {
			return nil, fmt.Errorf("config: SUPABASE_URL must be a valid http or https URL")
		}
	}
	cfg.SupabaseJWKSURL = getString(lookup, "SUPABASE_JWKS_URL", "")
	if cfg.SupabaseJWKSURL != "" {
		if err := validateURL(cfg.SupabaseJWKSURL); err != nil {
			return nil, fmt.Errorf("config: SUPABASE_JWKS_URL must be a valid http or https URL")
		}
	}
	cfg.SupabaseJWTSecret = getString(lookup, "SUPABASE_JWT_SECRET", "")

	if v, ok := lookup("AI_MAX_OUTPUT_TOKENS"); ok && strings.TrimSpace(v) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || parsed < 1 || parsed > 8192 {
			return nil, fmt.Errorf("config: AI_MAX_OUTPUT_TOKENS must be between 1 and 8192")
		}
		cfg.AIMaxOutputTokens = parsed
	}

	if v, ok := lookup("AI_TEMPERATURE"); ok && strings.TrimSpace(v) != "" {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || parsed < 0.0 || parsed > 2.0 {
			return nil, fmt.Errorf("config: AI_TEMPERATURE must be between 0.0 and 2.0")
		}
		cfg.AITemperature = parsed
	}

	if v, ok := lookup("AI_TIMEOUT"); ok && strings.TrimSpace(v) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("config: AI_TIMEOUT must be a positive duration")
		}
		cfg.AITimeout = d
	}

	if v, ok := lookup("AI_MAX_CONTEXT_MESSAGES"); ok && strings.TrimSpace(v) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil || parsed < 1 || parsed > 100 {
			return nil, fmt.Errorf("config: AI_MAX_CONTEXT_MESSAGES must be between 1 and 100")
		}
		cfg.AIMaxContextMessages = parsed
	}

	if v, ok := lookup("DB_MAX_CONNS"); ok && strings.TrimSpace(v) != "" {
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
		if err != nil || parsed < 1 {
			return nil, fmt.Errorf("config: DB_MAX_CONNS must be a positive integer")
		}
		cfg.DBMaxConns = int32(parsed)
		if cfg.DBMinConns > cfg.DBMaxConns {
			cfg.DBMinConns = cfg.DBMaxConns
		}
	}

	if v, ok := lookup("DB_MIN_CONNS"); ok && strings.TrimSpace(v) != "" {
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
		if err != nil || parsed < 0 || int32(parsed) > cfg.DBMaxConns {
			return nil, fmt.Errorf("config: DB_MIN_CONNS must be between 0 and DB_MAX_CONNS")
		}
		cfg.DBMinConns = int32(parsed)
	}

	if v, ok := lookup("SHUTDOWN_TIMEOUT"); ok && strings.TrimSpace(v) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("config: SHUTDOWN_TIMEOUT must be a positive duration")
		}
		cfg.ShutdownTimeout = d
	}

	return cfg, nil
}

// Address returns the host:port listen address for the HTTP server.
func (c *Config) Address() string {
	return fmt.Sprintf(":%d", c.AppPort)
}

// String returns a redacted representation of Config safe for logging.
func (c Config) String() string {
	hfTokenRedacted := "[UNSET]"
	if c.HFToken != "" {
		hfTokenRedacted = "[REDACTED]"
	}
	geminiKeyRedacted := "[UNSET]"
	if c.GeminiAPIKey != "" {
		geminiKeyRedacted = "[REDACTED]"
	}
	jwtSecretRedacted := "[UNSET]"
	if c.SupabaseJWTSecret != "" {
		jwtSecretRedacted = "[REDACTED]"
	}
	return fmt.Sprintf(
		"Config{AppEnv:%q, AppPort:%d, FrontendOrigin:%q, DatabaseURL:\"[REDACTED]\", SupabaseURL:%q, SupabaseJWTSecret:%q, AIProvider:%q, GeminiModelID:%q, GeminiAPIKey:%q, HFModelID:%q, HFBaseURL:%q, HFToken:%q}",
		c.AppEnv,
		c.AppPort,
		c.FrontendOrigin,
		c.SupabaseURL,
		jwtSecretRedacted,
		c.AIProvider,
		c.GeminiModelID,
		geminiKeyRedacted,
		c.HFModelID,
		c.HFBaseURL,
		hfTokenRedacted,
	)
}

// GoString prevents accidental credential printing with %#v.
func (c Config) GoString() string {
	return c.String()
}

func getString(lookup func(string) (string, bool), key, fallback string) string {
	if val, ok := lookup(key); ok {
		trimmed := strings.TrimSpace(val)
		if trimmed != "" {
			return trimmed
		}
	}
	return fallback
}

// ValidateDatabaseURL validates a PostgreSQL connection URL. Returned errors never contain the input.
func ValidateDatabaseURL(raw string) error {
	return validateDatabaseURL(strings.TrimSpace(raw))
}

// validateDatabaseURL validates the PostgreSQL URL without including raw input in any returned error.
func validateDatabaseURL(raw string) error {
	idx := strings.Index(raw, "://")
	if idx <= 0 {
		return ErrInvalidDatabaseURL
	}
	scheme := strings.ToLower(raw[:idx])
	if scheme != "postgres" && scheme != "postgresql" {
		return ErrInvalidDatabaseURL
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidDatabaseURL
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return ErrInvalidDatabaseURL
	}
	if parsed.User != nil {
		if pass, hasPass := parsed.User.Password(); hasPass && strings.TrimSpace(pass) == "" {
			return ErrInvalidDatabaseURL
		}
	}
	if p := parsed.Port(); p != "" {
		portNum, err := strconv.Atoi(p)
		if err != nil || portNum < 1 || portNum > 65535 {
			return ErrInvalidDatabaseURL
		}
	}
	return nil
}

func validateFrontendOrigin(origin string) error {
	if origin == "" || origin == "*" || strings.Contains(origin, "*") {
		return ErrInvalidOrigin
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return ErrInvalidOrigin
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return ErrInvalidOrigin
	}
	return nil
}

func validateURL(raw string) error {
	if raw == "" {
		return errors.New("empty url")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return errors.New("url must have http or https scheme and a host")
	}
	return nil
}
