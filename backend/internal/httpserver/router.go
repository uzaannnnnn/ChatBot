package httpserver

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/uzaannnnnn/ChatBot/backend/internal/auth"
	"github.com/uzaannnnnn/ChatBot/backend/internal/config"
	"github.com/uzaannnnnn/ChatBot/backend/internal/dto"
	"github.com/uzaannnnnn/ChatBot/backend/internal/handler"
	"github.com/uzaannnnnn/ChatBot/backend/internal/middleware"
	"golang.org/x/time/rate"
)

// Dependencies bundles the dependencies injected into the HTTP router.
type Dependencies struct {
	Config              *config.Config
	AuthVerifier        auth.Verifier
	HealthHandler       *handler.HealthHandler
	ConversationHandler *handler.ConversationHandler
	MessageHandler      *handler.MessageHandler
	ChatStreamHandler   *handler.ChatStreamHandler
	DisableRateLimit    bool
}

// NewRouter constructs a modular Gin engine with explicit middleware and route registration.
func NewRouter(deps Dependencies) *gin.Engine {
	if deps.Config != nil && deps.Config.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.HandleMethodNotAllowed = true

	router.Use(middleware.RequestID())
	router.Use(structuredLogger())
	router.Use(gin.Recovery())
	router.Use(middleware.SecurityHeaders())

	frontendOrigin := config.DefaultFrontendOrigin
	maxBodyBytes := config.DefaultMaxRequestBodyBytes
	if deps.Config != nil {
		if deps.Config.FrontendOrigin != "" {
			frontendOrigin = deps.Config.FrontendOrigin
		}
		if deps.Config.MaxRequestBodyBytes > 0 {
			maxBodyBytes = deps.Config.MaxRequestBodyBytes
		}
	}

	router.Use(middleware.CORS(frontendOrigin))
	router.Use(middleware.MaxBodySize(maxBodyBytes))

	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, dto.ErrorResponse{
			Error: dto.ErrorDetail{
				Code:    dto.ErrCodeRouteNotFound,
				Message: "The requested endpoint does not exist",
			},
		})
	})

	router.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusMethodNotAllowed, dto.ErrorResponse{
			Error: dto.ErrorDetail{
				Code:    dto.ErrCodeMethodNotAllowed,
				Message: "HTTP method is not allowed for this endpoint",
			},
		})
	})

	if deps.HealthHandler != nil {
		router.GET("/health", deps.HealthHandler.Health)
		router.GET("/ready", deps.HealthHandler.Ready)
	}

	v1 := router.Group("/api/v1")
	v1.Use(middleware.Auth(deps.AuthVerifier))

	if !deps.DisableRateLimit {
		apiLimiter := middleware.NewRateLimiter(rate.Limit(5), 30)
		v1.Use(apiLimiter.Middleware())
	}

	if deps.ConversationHandler != nil {
		v1.POST("/conversations", deps.ConversationHandler.Create)
		v1.GET("/conversations", deps.ConversationHandler.List)
		v1.GET("/conversations/:id", deps.ConversationHandler.Get)
		v1.PATCH("/conversations/:id", deps.ConversationHandler.UpdateTitle)
		v1.DELETE("/conversations/:id", deps.ConversationHandler.Delete)
	}
	if deps.MessageHandler != nil {
		v1.GET("/conversations/:id/messages", deps.MessageHandler.List)
		v1.POST("/conversations/:id/messages", deps.MessageHandler.Create)
	}
	if deps.ChatStreamHandler != nil {
		if !deps.DisableRateLimit {
			streamLimiter := middleware.NewRateLimiter(rate.Limit(0.5), 5)
			v1.POST("/conversations/:id/messages/stream", streamLimiter.Middleware(), deps.ChatStreamHandler.Stream)
		} else {
			v1.POST("/conversations/:id/messages/stream", deps.ChatStreamHandler.Stream)
		}
	}

	return router
}

func structuredLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		reqID := middleware.GetRequestID(c)
		userID, _ := middleware.UserID(c)

		attrs := []slog.Attr{
			slog.String("request_id", reqID),
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.String("client_ip", c.ClientIP()),
		}
		if userID != "" {
			attrs = append(attrs, slog.String("user_id", userID))
		}

		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}

		slog.LogAttrs(c.Request.Context(), level, "http request", attrs...)
	}
}
