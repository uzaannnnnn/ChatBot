package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uzaannnnnn/ChatBot/backend/internal/config"
)

const defaultHealthCheckPeriod = 1 * time.Minute

// NewPostgresPool initializes a pgxpool.Pool configured for PostgreSQL and Supabase Session Pooler,
// and verifies initial connectivity using Ping within the configured connection timeout.
func NewPostgresPool(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	if cfg == nil {
		return nil, errors.New("database: configuration is required")
	}

	poolCfg, err := BuildPoolConfig(cfg)
	if err != nil {
		return nil, err
	}

	connectTimeout := cfg.DBConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = config.DefaultDBConnectTimeout
	}

	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("database: failed to create connection pool: %w", sanitizeErr(err, cfg.DatabaseURL))
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: failed initial connectivity ping: %w", sanitizeErr(err, cfg.DatabaseURL))
	}

	return pool, nil
}

// BuildPoolConfig parses the connection string and applies pool sizing and lifecycle settings.
func BuildPoolConfig(cfg *config.Config) (*pgxpool.Config, error) {
	if cfg == nil || strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, errors.New("database: DATABASE_URL is required")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: invalid connection configuration: %w", sanitizeErr(err, cfg.DatabaseURL))
	}

	if cfg.DBMaxConns > 0 {
		poolCfg.MaxConns = cfg.DBMaxConns
	} else {
		poolCfg.MaxConns = config.DefaultDBMaxConns
	}

	if cfg.DBMinConns >= 0 && cfg.DBMinConns <= poolCfg.MaxConns {
		poolCfg.MinConns = cfg.DBMinConns
	} else if config.DefaultDBMinConns <= poolCfg.MaxConns {
		poolCfg.MinConns = config.DefaultDBMinConns
	} else {
		poolCfg.MinConns = poolCfg.MaxConns
	}

	if cfg.DBMaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.DBMaxConnLifetime
	} else {
		poolCfg.MaxConnLifetime = config.DefaultDBMaxConnLifetime
	}

	if cfg.DBMaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.DBMaxConnIdleTime
	} else {
		poolCfg.MaxConnIdleTime = config.DefaultDBMaxConnIdleTime
	}

	poolCfg.HealthCheckPeriod = defaultHealthCheckPeriod

	connectTimeout := cfg.DBConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = config.DefaultDBConnectTimeout
	}
	poolCfg.ConnConfig.ConnectTimeout = connectTimeout

	return poolCfg, nil
}

// SanitizeError strips the raw connection URL, password, and username from an
// error message so credentials are never logged. The returned error does not
// wrap the original to guarantee the raw message cannot be recovered by callers.
func SanitizeError(err error, rawURL string) error {
	return sanitizeErr(err, rawURL)
}

// sanitizeErr strips any raw connection URL or credentials from an error message so they are never logged.
func sanitizeErr(err error, rawURL string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if rawURL != "" {
		msg = strings.ReplaceAll(msg, rawURL, "[REDACTED_URL]")
		if parsed, parseErr := url.Parse(rawURL); parseErr == nil && parsed.User != nil {
			if pass, hasPass := parsed.User.Password(); hasPass && pass != "" {
				msg = strings.ReplaceAll(msg, pass, "[REDACTED]")
				if escaped := url.QueryEscape(pass); escaped != "" && escaped != pass {
					msg = strings.ReplaceAll(msg, escaped, "[REDACTED]")
				}
			}
			// pgconn connection errors embed "user=<name>"; Supabase pooler
			// usernames contain the project reference, so redact them too.
			if user := parsed.User.Username(); user != "" {
				msg = strings.ReplaceAll(msg, user, "[REDACTED_USER]")
			}
		}
	}
	return errors.New(msg)
}
