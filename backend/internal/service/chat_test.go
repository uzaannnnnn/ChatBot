package service_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/service"
)

const testOwnerID = "11111111-1111-4111-8111-111111111111"
const testConvID = "123e4567-e89b-12d3-a456-426614174000"

type mockStream struct {
	chunks []string
	idx    int
	err    error
}

func (m *mockStream) Recv() (ai.StreamChunk, error) {
	if m.err != nil {
		return ai.StreamChunk{}, m.err
	}
	if m.idx >= len(m.chunks) {
		return ai.StreamChunk{}, io.EOF
	}
	chunk := m.chunks[m.idx]
	m.idx++
	return ai.StreamChunk{Delta: chunk}, nil
}

func (m *mockStream) Close() error {
	return nil
}

type mockAIProvider struct {
	streamChatFunc func(ctx context.Context, req ai.ChatRequest) (ai.Stream, error)
	chatFunc       func(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error)
}

func (m *mockAIProvider) StreamChat(ctx context.Context, req ai.ChatRequest) (ai.Stream, error) {
	if m.streamChatFunc != nil {
		return m.streamChatFunc(ctx, req)
	}
	return &mockStream{chunks: []string{"test"}}, nil
}

func (m *mockAIProvider) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	if m.chatFunc != nil {
		return m.chatFunc(ctx, req)
	}
	return ai.ChatResponse{}, nil
}

type recordingMessageRepo struct {
	created []domain.NewMessage
	recent  []domain.Message
	err     error
}

func (r *recordingMessageRepo) CreateMessage(_ context.Context, _ string, msg domain.NewMessage) (domain.Message, error) {
	if r.err != nil {
		return domain.Message{}, r.err
	}
	r.created = append(r.created, msg)
	return domain.Message{
		ID:             fmtSprintf("msg-%d", len(r.created)),
		ConversationID: msg.ConversationID,
		Role:           msg.Role,
		Content:        msg.Content,
		CreatedAt:      time.Now(),
	}, nil
}

func (r *recordingMessageRepo) ListMessagesByConversationID(_ context.Context, _, _ string, _ domain.Page) ([]domain.Message, error) {
	return nil, nil
}

func (r *recordingMessageRepo) ListRecentMessages(_ context.Context, _, _ string, _ int) ([]domain.Message, error) {
	return r.recent, nil
}

type mockConvRepo struct{}

func (m *mockConvRepo) CreateConversation(_ context.Context, _, _ string) (domain.Conversation, error) {
	return domain.Conversation{}, nil
}
func (m *mockConvRepo) GetConversationByID(_ context.Context, _, _ string) (domain.Conversation, error) {
	return domain.Conversation{ID: testConvID}, nil
}
func (m *mockConvRepo) ListConversations(_ context.Context, _ string, _ domain.Page) ([]domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepo) UpdateConversationTitle(_ context.Context, _, _, _ string) (domain.Conversation, error) {
	return domain.Conversation{}, nil
}
func (m *mockConvRepo) DeleteConversation(_ context.Context, _, _ string) error {
	return nil
}

func fmtSprintf(f string, a ...any) string {
	return strings.Replace(f, "%d", "1", 1)
}

func TestChatService_StreamMessageSuccess(t *testing.T) {
	t.Parallel()

	msgRepo := &recordingMessageRepo{
		recent: []domain.Message{
			{ID: "m1", Role: domain.RoleUser, Content: "Hello"},
			{ID: "m2", Role: domain.RoleAssistant, Content: "Hi there!"},
		},
	}

	var capturedReq ai.ChatRequest
	mockAI := &mockAIProvider{
		streamChatFunc: func(_ context.Context, req ai.ChatRequest) (ai.Stream, error) {
			capturedReq = req
			return &mockStream{chunks: []string{"I ", "am ", "Go ", "bot."}}, nil
		},
	}

	svc := service.NewChatService(&mockConvRepo{}, msgRepo, mockAI, service.ChatServiceConfig{
		ModelID:        "test-model",
		SystemPrompt:   "Be helpful.",
		MaxTokens:      512,
		Temperature:    0.5,
		Timeout:        5 * time.Second,
		MaxContextMsgs: 10,
	})

	var streamedChunks []string
	userMsg, assistantMsg, err := svc.StreamMessage(
		context.Background(),
		testOwnerID,
		testConvID,
		"Tell me about Go",
		func(delta string) error {
			streamedChunks = append(streamedChunks, delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// 1. Verify user message was persisted
	if userMsg.Role != domain.RoleUser || userMsg.Content != "Tell me about Go" {
		t.Errorf("unexpected user message: %+v", userMsg)
	}

	// 2. Verify assistant message was persisted with full aggregated content
	if assistantMsg.Role != domain.RoleAssistant || assistantMsg.Content != "I am Go bot." {
		t.Errorf("unexpected assistant message: %+v", assistantMsg)
	}

	// 3. Verify exactly 2 messages were created: user first, assistant second
	if len(msgRepo.created) != 2 {
		t.Fatalf("expected 2 created messages, got %d", len(msgRepo.created))
	}
	if msgRepo.created[0].Role != domain.RoleUser || msgRepo.created[1].Role != domain.RoleAssistant {
		t.Errorf("unexpected creation order: %v, %v", msgRepo.created[0].Role, msgRepo.created[1].Role)
	}

	// 4. Verify context assembly
	if len(capturedReq.Messages) != 4 { // system + 2 history + 1 current user
		t.Fatalf("expected 4 messages in prompt, got %d", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "system" || capturedReq.Messages[0].Content != "Be helpful." {
		t.Errorf("expected system message, got %+v", capturedReq.Messages[0])
	}
	if capturedReq.Messages[3].Role != "user" || capturedReq.Messages[3].Content != "Tell me about Go" {
		t.Errorf("expected last user message, got %+v", capturedReq.Messages[3])
	}

	// 5. Verify streaming deltas
	if strings.Join(streamedChunks, "") != "I am Go bot." {
		t.Errorf("unexpected streamed chunks: %v", streamedChunks)
	}
}

func TestChatService_ProviderFailureLeavesUserMessagePersisted(t *testing.T) {
	t.Parallel()

	msgRepo := &recordingMessageRepo{}
	mockAI := &mockAIProvider{
		streamChatFunc: func(_ context.Context, _ ai.ChatRequest) (ai.Stream, error) {
			return nil, errors.New("huggingface 503 provider unavailable")
		},
	}

	svc := service.NewChatService(&mockConvRepo{}, msgRepo, mockAI, service.ChatServiceConfig{
		ModelID: "test-model",
	})

	userMsg, assistantMsg, err := svc.StreamMessage(context.Background(), testOwnerID, testConvID, "User prompt", nil)
	if err == nil {
		t.Fatal("expected provider error, got nil")
	}

	// User message was persisted
	if userMsg.Content != "User prompt" {
		t.Errorf("expected userMsg to be returned, got %+v", userMsg)
	}
	if len(msgRepo.created) != 1 || msgRepo.created[0].Role != domain.RoleUser {
		t.Fatalf("expected exactly 1 user message created in DB, got %d", len(msgRepo.created))
	}

	// Assistant message was NOT persisted
	if assistantMsg.ID != "" {
		t.Errorf("expected empty assistantMsg, got %+v", assistantMsg)
	}
}

func TestChatService_ClientDisconnectAbortsAndDoesNotPersistAssistant(t *testing.T) {
	t.Parallel()

	msgRepo := &recordingMessageRepo{}
	mockAI := &mockAIProvider{
		streamChatFunc: func(_ context.Context, _ ai.ChatRequest) (ai.Stream, error) {
			return &mockStream{chunks: []string{"Token 1", "Token 2", "Token 3"}}, nil
		},
	}

	svc := service.NewChatService(&mockConvRepo{}, msgRepo, mockAI, service.ChatServiceConfig{
		ModelID: "test-model",
	})

	count := 0
	_, _, err := svc.StreamMessage(
		context.Background(),
		testOwnerID,
		testConvID,
		"Prompt",
		func(delta string) error {
			count++
			if count == 1 {
				// Client disconnects after first chunk
				return errors.New("client disconnected")
			}
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "client disconnected") {
		t.Fatalf("expected client disconnected error, got %v", err)
	}

	// User message is persisted, but assistant message is NOT persisted
	if len(msgRepo.created) != 1 || msgRepo.created[0].Role != domain.RoleUser {
		t.Fatalf("expected only user message persisted on client disconnect, got %d created", len(msgRepo.created))
	}
}

func TestChatService_ConversationNotFound(t *testing.T) {
	t.Parallel()

	msgRepo := &recordingMessageRepo{
		err: domain.ErrConversationNotFound,
	}
	svc := service.NewChatService(&mockConvRepo{}, msgRepo, &mockAIProvider{}, service.ChatServiceConfig{})

	_, _, err := svc.StreamMessage(context.Background(), testOwnerID, testConvID, "Hello", nil)
	if !errors.Is(err, domain.ErrConversationNotFound) {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}
	if len(msgRepo.created) != 0 {
		t.Errorf("expected 0 messages created, got %d", len(msgRepo.created))
	}
}

func TestChatService_AIUnavailable(t *testing.T) {
	t.Parallel()

	msgRepo := &recordingMessageRepo{}
	// Provider is nil
	svc := service.NewChatService(&mockConvRepo{}, msgRepo, nil, service.ChatServiceConfig{})

	userMsg, _, err := svc.StreamMessage(context.Background(), testOwnerID, testConvID, "Hello", nil)
	if !errors.Is(err, domain.ErrAIUnavailable) {
		t.Fatalf("expected ErrAIUnavailable, got %v", err)
	}
	// User message is still created
	if len(msgRepo.created) != 1 {
		t.Errorf("expected user message created before provider check, got %d", len(msgRepo.created))
	}
	if userMsg.Content != "Hello" {
		t.Errorf("unexpected user message: %+v", userMsg)
	}
}

func TestChatService_WithGeminiProviderAdapter(t *testing.T) {
	t.Parallel()

	// Verify that any ai.Provider (including gemini.Client) satisfies ChatService requirements
	msgRepo := &recordingMessageRepo{}
	mockAI := &mockAIProvider{
		streamChatFunc: func(_ context.Context, req ai.ChatRequest) (ai.Stream, error) {
			if req.Model != "gemini-3.5-flash" {
				t.Errorf("expected model gemini-3.5-flash, got %s", req.Model)
			}
			return &mockStream{chunks: []string{"Hello ", "from ", "Gemini!"}}, nil
		},
	}

	svc := service.NewChatService(&mockConvRepo{}, msgRepo, mockAI, service.ChatServiceConfig{
		ModelID: "gemini-3.5-flash",
	})

	var collected strings.Builder
	userMsg, assistantMsg, err := svc.StreamMessage(
		context.Background(),
		testOwnerID,
		testConvID,
		"Test input",
		func(delta string) error {
			collected.WriteString(delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if userMsg.Content != "Test input" {
		t.Errorf("expected userMsg 'Test input', got %q", userMsg.Content)
	}
	if assistantMsg.Content != "Hello from Gemini!" {
		t.Errorf("expected assistantMsg 'Hello from Gemini!', got %q", assistantMsg.Content)
	}
	if collected.String() != "Hello from Gemini!" {
		t.Errorf("expected collected stream 'Hello from Gemini!', got %q", collected.String())
	}
	if len(msgRepo.created) != 2 {
		t.Errorf("expected 2 messages created, got %d", len(msgRepo.created))
	}
}

func TestChatService_Transient503RetryDoesNotDuplicateUserMessage(t *testing.T) {
	t.Parallel()

	msgRepo := &recordingMessageRepo{}
	attempts := 0
	mockAI := &mockAIProvider{
		streamChatFunc: func(_ context.Context, _ ai.ChatRequest) (ai.Stream, error) {
			attempts++
			if attempts == 1 {
				return nil, errors.New("gemini API error (HTTP 503): unavailable")
			}
			return &mockStream{chunks: []string{"Recovered response"}}, nil
		},
	}

	svc := service.NewChatService(&mockConvRepo{}, msgRepo, mockAI, service.ChatServiceConfig{
		ModelID: "gemini-3.1-flash-lite",
		Timeout: 5 * time.Second,
	})

	userMsg, assistantMsg, err := svc.StreamMessage(context.Background(), testOwnerID, testConvID, "Retry prompt", nil)
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
	if len(msgRepo.created) != 2 {
		t.Fatalf("expected exactly 2 persisted messages (1 user, 1 assistant), got %d", len(msgRepo.created))
	}
	if userMsg.Content != "Retry prompt" || assistantMsg.Content != "Recovered response" {
		t.Errorf("unexpected messages: user=%+v assistant=%+v", userMsg, assistantMsg)
	}
}

func TestChatService_NonTransientErrorDoesNotRetry(t *testing.T) {
	t.Parallel()

	msgRepo := &recordingMessageRepo{}
	attempts := 0
	mockAI := &mockAIProvider{
		streamChatFunc: func(_ context.Context, _ ai.ChatRequest) (ai.Stream, error) {
			attempts++
			return nil, errors.New("gemini API error (HTTP 400): bad request")
		},
	}

	svc := service.NewChatService(&mockConvRepo{}, msgRepo, mockAI, service.ChatServiceConfig{
		ModelID: "gemini-3.1-flash-lite",
		Timeout: 5 * time.Second,
	})

	_, _, err := svc.StreamMessage(context.Background(), testOwnerID, testConvID, "Bad prompt", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry for HTTP 400), got %d", attempts)
	}
	if len(msgRepo.created) != 1 {
		t.Errorf("expected only 1 user message persisted, got %d", len(msgRepo.created))
	}
}
