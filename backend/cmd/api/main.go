package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	"github.com/uzaannnnnn/ChatBot/backend/internal/ai/gemini"
	"github.com/uzaannnnnn/ChatBot/backend/internal/ai/huggingface"
	"github.com/uzaannnnnn/ChatBot/backend/internal/auth"
	"github.com/uzaannnnnn/ChatBot/backend/internal/config"
	"github.com/uzaannnnnn/ChatBot/backend/internal/database"
	"github.com/uzaannnnnn/ChatBot/backend/internal/handler"
	"github.com/uzaannnnnn/ChatBot/backend/internal/httpserver"
	"github.com/uzaannnnnn/ChatBot/backend/internal/repository/postgres"
	"github.com/uzaannnnnn/ChatBot/backend/internal/service"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	idleTimeout       = 60 * time.Second
	// writeTimeout is intentionally set to 0 (disabled) because future Server-Sent Events (SSE)
	// streaming endpoints require long-lived response streams that must not be terminated
	// by a short global HTTP server WriteTimeout. Request body sizes and upstream/per-route
	// context deadlines are enforced separately.
	writeTimeout   = 0
	maxHeaderBytes = 1 << 20 // 1 MiB
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("application terminated with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger.Info("configuration loaded",
		"env", cfg.AppEnv,
		"port", cfg.AppPort,
		"frontend_origin", cfg.FrontendOrigin,
	)

	sigCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	pool, err := database.NewPostgresPool(sigCtx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		logger.Info("closing postgresql connection pool")
		pool.Close()
	}()

	logger.Info("postgresql connection pool initialized and verified",
		"max_conns", cfg.DBMaxConns,
		"min_conns", cfg.DBMinConns,
	)

	conversationRepo := postgres.NewConversationRepository(pool)
	messageRepo := postgres.NewMessageRepository(pool)

	conversationSvc := service.NewConversationService(conversationRepo)
	messageSvc := service.NewMessageService(messageRepo)

	var aiProvider ai.Provider
	var activeModelID string

	switch cfg.AIProvider {
	case "gemini":
		activeModelID = cfg.GeminiModelID
		if cfg.GeminiAPIKey != "" {
			aiProvider = gemini.NewClient(gemini.ClientOptions{
				BaseURL:      cfg.GeminiBaseURL,
				APIKey:       cfg.GeminiAPIKey,
				DefaultModel: cfg.GeminiModelID,
			})
			logger.Info("gemini ai provider initialized", "provider", "gemini", "model", cfg.GeminiModelID, "base_url", cfg.GeminiBaseURL)
		} else {
			logger.Warn("gemini api key not configured (GEMINI_API_KEY is empty); streaming completions will return 503")
		}
	case "huggingface":
		activeModelID = cfg.HFModelID
		if cfg.HFToken != "" {
			aiProvider = huggingface.NewClient(huggingface.ClientOptions{
				BaseURL:      cfg.HFBaseURL,
				Token:        cfg.HFToken,
				DefaultModel: cfg.HFModelID,
				Provider:     cfg.HFProvider,
			})
			logger.Info("huggingface ai provider initialized", "provider", "huggingface", "model", cfg.HFModelID, "base_url", cfg.HFBaseURL)
		} else {
			logger.Warn("huggingface provider token not configured (HF_TOKEN is empty); streaming completions will return 503")
		}
	default:
		logger.Warn("unknown ai provider configured", "provider", cfg.AIProvider)
	}

	chatSvc := service.NewChatService(conversationRepo, messageRepo, aiProvider, service.ChatServiceConfig{
		ModelID:        activeModelID,
		SystemPrompt:   cfg.AISystemPrompt,
		MaxTokens:      cfg.AIMaxOutputTokens,
		Temperature:    cfg.AITemperature,
		Timeout:        cfg.AITimeout,
		MaxContextMsgs: cfg.AIMaxContextMessages,
	})

	var authVerifier auth.Verifier
	if cfg.SupabaseURL != "" || cfg.SupabaseJWKSURL != "" || cfg.SupabaseJWTSecret != "" {
		authVerifier = auth.NewSupabaseVerifier(auth.SupabaseVerifierOptions{
			SupabaseURL: cfg.SupabaseURL,
			JWKSURL:     cfg.SupabaseJWKSURL,
			JWTSecret:   cfg.SupabaseJWTSecret,
		})
		logger.Info("supabase jwt verifier initialized", "supabase_url", cfg.SupabaseURL)
	} else {
		logger.Warn("supabase auth not configured (SUPABASE_URL / SUPABASE_JWT_SECRET empty); protected endpoints will return 503")
	}

	healthHandler := handler.NewHealthHandler(pool, cfg.ReadinessTimeout)
	conversationHandler := handler.NewConversationHandler(conversationSvc)
	messageHandler := handler.NewMessageHandler(messageSvc)
	chatStreamHandler := handler.NewChatStreamHandler(chatSvc)

	router := httpserver.NewRouter(httpserver.Dependencies{
		Config:              cfg,
		AuthVerifier:        authVerifier,
		HealthHandler:       healthHandler,
		ConversationHandler: conversationHandler,
		MessageHandler:      messageHandler,
		ChatStreamHandler:   chatStreamHandler,
	})

	srv := &http.Server{
		Addr:              cfg.Address(),
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("starting http server", "addr", srv.Addr)
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErrCh <- serveErr
		}
		close(serverErrCh)
	}()

	select {
	case serveErr := <-serverErrCh:
		if serveErr != nil {
			return serveErr
		}
		return nil
	case <-sigCtx.Done():
		logger.Info("shutdown signal received, initiating graceful shutdown")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	logger.Info("http server stopped gracefully")
	return nil
}
