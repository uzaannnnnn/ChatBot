// Package tools implements the backend-controlled AI tool registry,
// execution guards, usage tracking, and registered tool handlers.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
)

const (
	DefaultToolTimeout = 45 * time.Second
)

var (
	ErrUnknownTool      = errors.New("tools: requested tool is not registered")
	ErrInvalidToolInput = errors.New("tools: invalid tool arguments")
	ErrToolRateLimited  = errors.New("tools: rate limit exceeded for tool execution")
	ErrMaxCallsExceeded = errors.New("tools: maximum tool executions per request exceeded")
)

// ExecutionContext provides request-scoped identity and metadata to a tool.
type ExecutionContext struct {
	OwnerID        string
	ConversationID string
}

// ExecutionResult is the structured output of executing a backend tool.
type ExecutionResult struct {
	// ModelOutput is returned to the AI model as the function_result payload.
	// It must never contain credentials or raw Base64 image data.
	ModelOutput map[string]any
	// Attachment is set when the tool produces a persisted/uploaded media attachment.
	Attachment *GeneratedAttachment
}

// GeneratedAttachment holds both the DB persistence record and the SSE-ready attachment.
type GeneratedAttachment struct {
	NewRecord  domain.NewMessageAttachment
	Attachment domain.MessageAttachment
}

// Tool defines a single backend-controlled capability exposed to the AI model.
type Tool interface {
	Name() string
	Definition() ai.ToolDefinition
	Execute(ctx context.Context, execCtx ExecutionContext, rawArgs json.RawMessage) (ExecutionResult, error)
}

// UsageSnapshot captures cumulative tool execution metrics.
type UsageSnapshot struct {
	Invocations int64
	Successes   int64
	Failures    int64
}

// Registry manages registered server-side tools with timeout enforcement and usage tracking.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	order   []string
	timeout time.Duration

	invocations atomic.Int64
	successes   atomic.Int64
	failures    atomic.Int64
}

// NewRegistry creates an empty tool registry with the given per-execution timeout.
func NewRegistry(timeout time.Duration) *Registry {
	if timeout <= 0 {
		timeout = DefaultToolTimeout
	}
	return &Registry{
		tools:   make(map[string]Tool),
		timeout: timeout,
	}
}

// Register adds a tool to the registry. Duplicate names overwrite the previous entry.
func (r *Registry) Register(t Tool) {
	if t == nil || t.Name() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; !exists {
		r.order = append(r.order, t.Name())
	}
	r.tools[t.Name()] = t
}

// Definitions returns all registered tool schemas in registration order.
func (r *Registry) Definitions() []ai.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ai.ToolDefinition, 0, len(r.order))
	for _, name := range r.order {
		if t, ok := r.tools[name]; ok {
			defs = append(defs, t.Definition())
		}
	}
	return defs
}

// HasTools reports whether at least one tool is registered.
func (r *Registry) HasTools() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools) > 0
}

// Execute invokes a registered tool by name with timeout enforcement and usage tracking.
func (r *Registry) Execute(ctx context.Context, execCtx ExecutionContext, call ai.ToolCall) (ExecutionResult, error) {
	r.mu.RLock()
	tool, ok := r.tools[call.Name]
	r.mu.RUnlock()
	if !ok {
		r.failures.Add(1)
		return ExecutionResult{}, fmt.Errorf("%w: %s", ErrUnknownTool, call.Name)
	}

	r.invocations.Add(1)
	start := time.Now()

	toolCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	res, err := tool.Execute(toolCtx, execCtx, call.Arguments)
	latency := time.Since(start)
	if err != nil {
		r.failures.Add(1)
		slog.Warn("tool execution failed",
			"tool", call.Name,
			"owner_id", execCtx.OwnerID,
			"conversation_id", execCtx.ConversationID,
			"latency", latency,
			"error", err,
		)
		return ExecutionResult{}, err
	}

	r.successes.Add(1)
	slog.Info("tool execution succeeded",
		"tool", call.Name,
		"owner_id", execCtx.OwnerID,
		"conversation_id", execCtx.ConversationID,
		"latency", latency,
	)
	return res, nil
}

// Usage returns cumulative tool invocation metrics.
func (r *Registry) Usage() UsageSnapshot {
	return UsageSnapshot{
		Invocations: r.invocations.Load(),
		Successes:   r.successes.Load(),
		Failures:    r.failures.Load(),
	}
}
