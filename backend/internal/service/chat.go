package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	"github.com/uzaannnnnn/ChatBot/backend/internal/ai/tools"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/repository"
	"github.com/uzaannnnnn/ChatBot/backend/internal/storage"
)

const (
	// MaxToolCallsPerRequest bounds backend tool executions to prevent loops and runaway quota usage.
	MaxToolCallsPerRequest = 1

	fallbackImageCaption       = "Here is your generated image."
	pseudoToolFallbackResponse = "I couldn't generate an image for that request because the image tool was not invoked properly. Please try again with a clear image description."
)

// ToolStatusEvent describes the lifecycle state of a backend tool execution.
type ToolStatusEvent struct {
	Tool    string
	Status  string // "running" | "completed" | "failed"
	Message string
}

// StreamCallbacks receives incremental events during an assistant turn.
type StreamCallbacks struct {
	OnDelta      func(delta string) error
	OnToolStatus func(event ToolStatusEvent) error
	OnAttachment func(attachment domain.MessageAttachment) error
}

// ChatServiceConfig sets the AI generation and context window settings.
type ChatServiceConfig struct {
	ModelID        string
	SystemPrompt   string
	MaxTokens      int
	Temperature    float64
	Timeout        time.Duration
	MaxContextMsgs int
}

// ChatService orchestrates conversation persistence, tool execution, and AI streaming generation.
type ChatService struct {
	convRepo       repository.ConversationRepository
	msgRepo        repository.MessageRepository
	attachmentRepo repository.AttachmentRepository
	storage        storage.ObjectStorage
	toolRegistry   *tools.Registry
	aiProvider     ai.Provider
	cfg            ChatServiceConfig
}

// NewChatService constructs a ChatService with the required dependencies and configuration.
func NewChatService(
	convRepo repository.ConversationRepository,
	msgRepo repository.MessageRepository,
	aiProvider ai.Provider,
	cfg ChatServiceConfig,
) *ChatService {
	if cfg.MaxContextMsgs <= 0 {
		cfg.MaxContextMsgs = 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 2048
	}
	svc := &ChatService{
		convRepo:   convRepo,
		msgRepo:    msgRepo,
		aiProvider: aiProvider,
		cfg:        cfg,
	}
	if attRepo, ok := msgRepo.(repository.AttachmentRepository); ok {
		svc.attachmentRepo = attRepo
	}
	return svc
}

// WithToolRegistry attaches a server-side tool registry for structured function calling.
func (s *ChatService) WithToolRegistry(registry *tools.Registry) *ChatService {
	s.toolRegistry = registry
	return s
}

// WithAttachmentStore configures atomic attachment persistence and orphan storage cleanup.
func (s *ChatService) WithAttachmentStore(attRepo repository.AttachmentRepository, store storage.ObjectStorage) *ChatService {
	if attRepo != nil {
		s.attachmentRepo = attRepo
	}
	s.storage = store
	return s
}

// StreamMessage validates the input, persists the incoming user message, assembles
// bounded context, streams tokens from the AI provider, invokes onChunk on each token,
// and upon successful complete generation, persists the completed assistant response.
func (s *ChatService) StreamMessage(
	ctx context.Context,
	ownerID string,
	conversationID string,
	userContent string,
	onChunk func(delta string) error,
) (domain.Message, domain.Message, error) {
	return s.StreamMessageWithEvents(ctx, ownerID, conversationID, userContent, StreamCallbacks{
		OnDelta: onChunk,
	})
}

// StreamMessageWithEvents executes a full assistant turn supporting text streaming,
// structured backend tool calls, attachment emission, and atomic persistence.
func (s *ChatService) StreamMessageWithEvents(
	ctx context.Context,
	ownerID string,
	conversationID string,
	userContent string,
	callbacks StreamCallbacks,
) (domain.Message, domain.Message, error) {
	if !domain.IsValidUUID(ownerID) {
		return domain.Message{}, domain.Message{}, domain.ErrUnauthorized
	}
	if !domain.IsValidUUID(conversationID) {
		return domain.Message{}, domain.Message{}, domain.ErrInvalidID
	}
	if err := validateContent(userContent); err != nil {
		return domain.Message{}, domain.Message{}, err
	}

	// 1. Persist user message first (enforces conversation ownership)
	userMsg, err := s.msgRepo.CreateMessage(ctx, ownerID, domain.NewMessage{
		ConversationID: conversationID,
		Role:           domain.RoleUser,
		Content:        userContent,
	})
	if err != nil {
		return domain.Message{}, domain.Message{}, err
	}

	// 2. Check if AI provider is available
	if s.aiProvider == nil {
		return userMsg, domain.Message{}, domain.ErrAIUnavailable
	}

	// 3. Assemble bounded conversation context (excluding the new userMsg)
	aiMsgs, err := s.buildContextMessages(ctx, ownerID, conversationID, userMsg)
	if err != nil {
		return userMsg, domain.Message{}, err
	}

	// 4. First AI generation turn (with registered tools if available)
	genCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	chatReq := ai.ChatRequest{
		Model:       s.cfg.ModelID,
		Messages:    aiMsgs,
		MaxTokens:   s.cfg.MaxTokens,
		Temperature: s.cfg.Temperature,
		Stream:      true,
	}
	if s.toolRegistry != nil && s.toolRegistry.HasTools() {
		chatReq.Tools = s.toolRegistry.Definitions()
	}

	turnResult, err := s.consumeProviderTurn(genCtx, chatReq, callbacks.OnDelta)
	if err != nil {
		return userMsg, domain.Message{}, err
	}

	var (
		fullText       strings.Builder
		newAttachments []domain.NewMessageAttachment
		uploadedPaths  []string
		emittedAtts    []domain.MessageAttachment
	)
	fullText.WriteString(turnResult.text)

	cleanupOrphans := func() {
		if len(uploadedPaths) > 0 && s.storage != nil {
			if delErr := s.storage.DeleteObjects(context.Background(), uploadedPaths); delErr != nil {
				slog.Warn("failed to delete orphaned storage objects", "paths", uploadedPaths, "error", delErr)
			}
		}
	}

	// 5. Execute at most MaxToolCallsPerRequest structured tool calls
	if len(turnResult.toolCalls) > 0 && s.toolRegistry != nil {
		calls := turnResult.toolCalls
		if len(calls) > MaxToolCallsPerRequest {
			calls = calls[:MaxToolCallsPerRequest]
		}

		call := calls[0]
		if callbacks.OnToolStatus != nil {
			if statusErr := callbacks.OnToolStatus(ToolStatusEvent{
				Tool:   call.Name,
				Status: "running",
			}); statusErr != nil {
				return userMsg, domain.Message{}, statusErr
			}
		}

		execResult, execErr := s.toolRegistry.Execute(genCtx, tools.ExecutionContext{
			OwnerID:        ownerID,
			ConversationID: conversationID,
		}, call)

		if execErr != nil {
			userErrMsg := tools.UserFacingToolError(execErr)
			if callbacks.OnToolStatus != nil {
				if statusErr := callbacks.OnToolStatus(ToolStatusEvent{
					Tool:    call.Name,
					Status:  "failed",
					Message: userErrMsg,
				}); statusErr != nil {
					return userMsg, domain.Message{}, statusErr
				}
			}

			// Always append a truthful explanation (even if preamble text preceded the tool call)
			// so reloaded history reflects that image generation did not succeed.
			failureDelta := userErrMsg
			if strings.TrimSpace(fullText.String()) != "" {
				failureDelta = "\n\n" + userErrMsg
			}
			fullText.WriteString(failureDelta)
			if callbacks.OnDelta != nil {
				if deltaErr := callbacks.OnDelta(failureDelta); deltaErr != nil {
					return userMsg, domain.Message{}, deltaErr
				}
			}
		} else {
			if execResult.Attachment != nil {
				uploadedPaths = append(uploadedPaths, execResult.Attachment.NewRecord.StoragePath)
				newAttachments = append(newAttachments, execResult.Attachment.NewRecord)
				emittedAtts = append(emittedAtts, execResult.Attachment.Attachment)
			}

			if callbacks.OnToolStatus != nil {
				if statusErr := callbacks.OnToolStatus(ToolStatusEvent{
					Tool:   call.Name,
					Status: "completed",
				}); statusErr != nil {
					cleanupOrphans()
					return userMsg, domain.Message{}, statusErr
				}
			}

			if execResult.Attachment != nil && callbacks.OnAttachment != nil {
				if attErr := callbacks.OnAttachment(execResult.Attachment.Attachment); attErr != nil {
					cleanupOrphans()
					return userMsg, domain.Message{}, attErr
				}
			}

			// Follow-up model turn with function_result (NO tools passed -> prevents infinite tool loops)
			followUpReq := ai.ChatRequest{
				Model:                 s.cfg.ModelID,
				PreviousInteractionID: turnResult.interactionID,
				ToolResults: []ai.ToolResult{{
					CallID: call.ID,
					Name:   call.Name,
					Result: execResult.ModelOutput,
				}},
				MaxTokens:   s.cfg.MaxTokens,
				Temperature: s.cfg.Temperature,
				Stream:      true,
			}
			// If provider didn't supply an interaction ID, pass fallback messages context
			if followUpReq.PreviousInteractionID == "" {
				followUpReq.Messages = append(aiMsgs, ai.Message{
					Role:    "user",
					Content: "The image has been generated successfully and displayed to the user. Provide a brief, helpful one-sentence caption.",
				})
				followUpReq.ToolResults = nil
			}

			followUpTurn, followUpErr := s.consumeProviderTurn(genCtx, followUpReq, callbacks.OnDelta)
			if followUpErr != nil {
				if errors.Is(followUpErr, context.Canceled) {
					cleanupOrphans()
					return userMsg, domain.Message{}, followUpErr
				}
				slog.Warn("follow-up turn after tool execution failed; using fallback caption", "error", followUpErr)
			} else if strings.TrimSpace(followUpTurn.text) != "" {
				if fullText.Len() > 0 {
					fullText.WriteString("\n\n")
				}
				fullText.WriteString(followUpTurn.text)
			}

			if strings.TrimSpace(fullText.String()) == "" && len(newAttachments) > 0 {
				fullText.WriteString(fallbackImageCaption)
				if callbacks.OnDelta != nil {
					if deltaErr := callbacks.OnDelta(fallbackImageCaption); deltaErr != nil {
						cleanupOrphans()
						return userMsg, domain.Message{}, deltaErr
					}
				}
			}
		}
	}

	completedText := fullText.String()

	// Guard against unsupported pseudo-tool JSON (e.g. dalle.text2im) emitted as plain text
	if len(newAttachments) == 0 && ai.ContainsPseudoToolJSON(completedText) {
		completedText = pseudoToolFallbackResponse
	}

	if strings.TrimSpace(completedText) == "" {
		cleanupOrphans()
		return userMsg, domain.Message{}, fmt.Errorf("ai provider returned empty response")
	}

	// 6. Persist completed assistant message (and attachments atomically if present)
	var assistantMsg domain.Message
	if len(newAttachments) > 0 && s.attachmentRepo != nil {
		assistantMsg, err = s.attachmentRepo.CreateMessageWithAttachments(ctx, ownerID, domain.NewMessage{
			ConversationID: conversationID,
			Role:           domain.RoleAssistant,
			Content:        completedText,
		}, newAttachments)
	} else {
		assistantMsg, err = s.msgRepo.CreateMessage(ctx, ownerID, domain.NewMessage{
			ConversationID: conversationID,
			Role:           domain.RoleAssistant,
			Content:        completedText,
		})
	}
	if err != nil {
		cleanupOrphans()
		return userMsg, domain.Message{}, err
	}

	// Preserve signed URLs on the returned domain.Message
	if len(emittedAtts) > 0 && len(assistantMsg.Attachments) == len(emittedAtts) {
		for i := range assistantMsg.Attachments {
			assistantMsg.Attachments[i].URL = emittedAtts[i].URL
		}
	}

	return userMsg, assistantMsg, nil
}

type providerTurnResult struct {
	interactionID string
	text          string
	toolCalls     []ai.ToolCall
}

func (s *ChatService) consumeProviderTurn(
	genCtx context.Context,
	chatReq ai.ChatRequest,
	onDelta func(delta string) error,
) (providerTurnResult, error) {
	var (
		stream ai.Stream
		err    error
	)
	for attempt := 0; attempt < 2; attempt++ {
		stream, err = s.aiProvider.StreamChat(genCtx, chatReq)
		if err == nil {
			break
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return providerTurnResult{}, err
		}
		// Only retry transient availability errors before any chunk is received
		if attempt == 0 && strings.Contains(err.Error(), "HTTP 503") {
			select {
			case <-time.After(300 * time.Millisecond):
				continue
			case <-genCtx.Done():
				return providerTurnResult{}, genCtx.Err()
			}
		}
		return providerTurnResult{}, fmt.Errorf("%w: %w", domain.ErrAIProvider, err)
	}
	defer stream.Close()

	var (
		fullResponse  strings.Builder
		interactionID string
		toolCalls     []ai.ToolCall
	)
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return providerTurnResult{}, err
			}
			return providerTurnResult{}, fmt.Errorf("%w: %w", domain.ErrAIProvider, err)
		}

		if chunk.InteractionID != "" {
			interactionID = chunk.InteractionID
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.ToolCalls...)
		}
		if chunk.Delta != "" {
			fullResponse.WriteString(chunk.Delta)
			if onDelta != nil {
				if chunkErr := onDelta(chunk.Delta); chunkErr != nil {
					return providerTurnResult{}, chunkErr
				}
			}
		}
	}

	return providerTurnResult{
		interactionID: interactionID,
		text:          fullResponse.String(),
		toolCalls:     toolCalls,
	}, nil
}

func (s *ChatService) buildContextMessages(
	ctx context.Context,
	ownerID string,
	conversationID string,
	userMsg domain.Message,
) ([]ai.Message, error) {
	historyLimit := s.cfg.MaxContextMsgs - 1
	var history []domain.Message
	if historyLimit > 0 {
		recent, err := s.msgRepo.ListRecentMessages(ctx, ownerID, conversationID, historyLimit+1)
		if err != nil && !errors.Is(err, domain.ErrConversationNotFound) {
			return nil, err
		}
		for _, m := range recent {
			if m.ID != userMsg.ID {
				history = append(history, m)
			}
		}
		if len(history) > historyLimit {
			history = history[len(history)-historyLimit:]
		}
	}

	aiMsgs := make([]ai.Message, 0, len(history)+2)
	if s.cfg.SystemPrompt != "" {
		aiMsgs = append(aiMsgs, ai.Message{
			Role:    "system",
			Content: s.cfg.SystemPrompt,
		})
	}
	for _, m := range history {
		aiMsgs = append(aiMsgs, ai.Message{
			Role:    string(m.Role),
			Content: m.Content,
		})
	}
	aiMsgs = append(aiMsgs, ai.Message{
		Role:    "user",
		Content: userMsg.Content,
	})
	return aiMsgs, nil
}
