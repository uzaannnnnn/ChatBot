package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/repository"
)

// ChatServiceConfig sets the AI generation and context window settings.
type ChatServiceConfig struct {
	ModelID        string
	SystemPrompt   string
	MaxTokens      int
	Temperature    float64
	Timeout        time.Duration
	MaxContextMsgs int
}

// ChatService orchestrates conversation persistence and AI streaming generation.
type ChatService struct {
	convRepo   repository.ConversationRepository
	msgRepo    repository.MessageRepository
	aiProvider ai.Provider
	cfg        ChatServiceConfig
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
	return &ChatService{
		convRepo:   convRepo,
		msgRepo:    msgRepo,
		aiProvider: aiProvider,
		cfg:        cfg,
	}
}

// StreamMessage validates the input, persists the incoming user message, assembles
// bounded context, streams tokens from the AI provider, invokes onChunk on each token,
// and upon successful complete generation, persists the completed assistant response.
//
// Failure Semantics:
//   - User message is persisted before calling the AI provider.
//   - Assistant message is persisted ONLY after the stream completes successfully.
//   - If AI generation fails, the user message remains saved, and no incomplete
//     assistant message is stored.
//   - If the client disconnects (onChunk returns an error), generation is aborted
//     and no assistant message is stored.
func (s *ChatService) StreamMessage(
	ctx context.Context,
	ownerID string,
	conversationID string,
	userContent string,
	onChunk func(delta string) error,
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
	historyLimit := s.cfg.MaxContextMsgs - 1
	var history []domain.Message
	if historyLimit > 0 {
		recent, err := s.msgRepo.ListRecentMessages(ctx, ownerID, conversationID, historyLimit+1)
		if err != nil && !errors.Is(err, domain.ErrConversationNotFound) {
			return userMsg, domain.Message{}, err
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

	// 4. Initiate AI provider stream with timeout and safe pre-stream retry on transient 503
	genCtx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	chatReq := ai.ChatRequest{
		Model:       s.cfg.ModelID,
		Messages:    aiMsgs,
		MaxTokens:   s.cfg.MaxTokens,
		Temperature: s.cfg.Temperature,
		Stream:      true,
	}

	var stream ai.Stream
	for attempt := 0; attempt < 2; attempt++ {
		stream, err = s.aiProvider.StreamChat(genCtx, chatReq)
		if err == nil {
			break
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return userMsg, domain.Message{}, err
		}
		// Only retry transient availability errors before any chunk is received
		if attempt == 0 && strings.Contains(err.Error(), "HTTP 503") {
			select {
			case <-time.After(300 * time.Millisecond):
				continue
			case <-genCtx.Done():
				return userMsg, domain.Message{}, genCtx.Err()
			}
		}
		return userMsg, domain.Message{}, fmt.Errorf("%w: %w", domain.ErrAIProvider, err)
	}
	defer stream.Close()

	// 5. Consume stream chunks and forward via onChunk
	var fullResponse strings.Builder
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return userMsg, domain.Message{}, err
			}
			return userMsg, domain.Message{}, fmt.Errorf("%w: %w", domain.ErrAIProvider, err)
		}

		if chunk.Delta != "" {
			fullResponse.WriteString(chunk.Delta)
			if onChunk != nil {
				if chunkErr := onChunk(chunk.Delta); chunkErr != nil {
					return userMsg, domain.Message{}, chunkErr
				}
			}
		}
	}

	completedText := fullResponse.String()
	if strings.TrimSpace(completedText) == "" {
		return userMsg, domain.Message{}, fmt.Errorf("ai provider returned empty response")
	}

	// 6. Persist completed assistant message
	assistantMsg, err := s.msgRepo.CreateMessage(ctx, ownerID, domain.NewMessage{
		ConversationID: conversationID,
		Role:           domain.RoleAssistant,
		Content:        completedText,
	})
	if err != nil {
		return userMsg, domain.Message{}, err
	}

	return userMsg, assistantMsg, nil
}
