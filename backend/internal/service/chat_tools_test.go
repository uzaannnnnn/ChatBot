package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	aiimage "github.com/uzaannnnnn/ChatBot/backend/internal/ai/image"
	"github.com/uzaannnnnn/ChatBot/backend/internal/ai/tools"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"golang.org/x/time/rate"
)

type scriptedStream struct {
	chunks []ai.StreamChunk
	idx    int
}

func (s *scriptedStream) Recv() (ai.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return ai.StreamChunk{}, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}

func (s *scriptedStream) Close() error { return nil }

type scriptedProvider struct {
	turns    [][]ai.StreamChunk
	requests []ai.ChatRequest
}

func (p *scriptedProvider) Chat(ctx context.Context, req ai.ChatRequest) (ai.ChatResponse, error) {
	return ai.ChatResponse{}, nil
}

func (p *scriptedProvider) StreamChat(ctx context.Context, req ai.ChatRequest) (ai.Stream, error) {
	p.requests = append(p.requests, req)
	turnIdx := len(p.requests) - 1
	if turnIdx >= len(p.turns) {
		return &scriptedStream{}, nil
	}
	return &scriptedStream{chunks: p.turns[turnIdx]}, nil
}

type stubImageProvider struct {
	data []byte
	err  error
}

func (s *stubImageProvider) Generate(ctx context.Context, req aiimage.GenerateRequest) (aiimage.GenerateResult, error) {
	if s.err != nil {
		return aiimage.GenerateResult{}, s.err
	}
	return aiimage.GenerateResult{
		Data:     s.data,
		MIMEType: "image/png",
		Width:    32,
		Height:   32,
		Prompt:   req.Prompt,
	}, nil
}

type stubObjectStorage struct {
	uploaded []string
	deleted  []string
}

func (s *stubObjectStorage) UploadImage(ctx context.Context, objectPath string, data []byte, mimeType string) error {
	s.uploaded = append(s.uploaded, objectPath)
	return nil
}

func (s *stubObjectStorage) CreateSignedURL(ctx context.Context, objectPath string, ttl time.Duration) (string, error) {
	return "https://signed.example/" + objectPath, nil
}

func (s *stubObjectStorage) DeleteObjects(ctx context.Context, objectPaths []string) error {
	s.deleted = append(s.deleted, objectPaths...)
	return nil
}

type stubAttachmentRepo struct {
	messages        []domain.Message
	failAssistantDB bool
	paths           []string
}

func (r *stubAttachmentRepo) CreateMessage(ctx context.Context, ownerID string, msg domain.NewMessage) (domain.Message, error) {
	return r.CreateMessageWithAttachments(ctx, ownerID, msg, nil)
}

func (r *stubAttachmentRepo) CreateMessageWithAttachments(
	ctx context.Context,
	ownerID string,
	msg domain.NewMessage,
	attachments []domain.NewMessageAttachment,
) (domain.Message, error) {
	if msg.Role == domain.RoleAssistant && r.failAssistantDB {
		return domain.Message{}, domain.ErrInternal
	}
	saved := domain.Message{
		ID:             "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		ConversationID: msg.ConversationID,
		Role:           msg.Role,
		Content:        msg.Content,
		CreatedAt:      time.Now().UTC(),
	}
	for _, a := range attachments {
		saved.Attachments = append(saved.Attachments, domain.MessageAttachment{
			ID:          a.ID,
			MessageID:   saved.ID,
			Kind:        a.Kind,
			StoragePath: a.StoragePath,
			MIMEType:    a.MIMEType,
			Width:       a.Width,
			Height:      a.Height,
			Prompt:      a.Prompt,
			AltText:     a.AltText,
			CreatedAt:   time.Now().UTC(),
		})
	}
	r.messages = append(r.messages, saved)
	return saved, nil
}

func (r *stubAttachmentRepo) ListMessagesByConversationID(ctx context.Context, ownerID, conversationID string, page domain.Page) ([]domain.Message, error) {
	return r.messages, nil
}

func (r *stubAttachmentRepo) ListRecentMessages(ctx context.Context, ownerID, conversationID string, limit int) ([]domain.Message, error) {
	return r.messages, nil
}

func (r *stubAttachmentRepo) ListAttachmentStoragePathsByConversationID(ctx context.Context, ownerID, conversationID string) ([]string, error) {
	return r.paths, nil
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	img.Set(0, 0, color.RGBA{R: 10, G: 200, B: 100, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestChatService_ImageGenerationToolLoop(t *testing.T) {
	ownerID := "11111111-1111-1111-1111-111111111111"
	convID := "22222222-2222-2222-2222-222222222222"

	provider := &scriptedProvider{
		turns: [][]ai.StreamChunk{
			{
				{
					InteractionID: "inter_abc",
					ToolCalls: []ai.ToolCall{
						{ID: "call_1", Name: tools.ToolNameGenerateImage, Arguments: json.RawMessage(`{"prompt":"sunset ocean"}`)},
						// Second tool call must be ignored (max 1 per user request)
						{ID: "call_2", Name: tools.ToolNameGenerateImage, Arguments: json.RawMessage(`{"prompt":"ignored"}`)},
					},
				},
			},
			{
				{Delta: "Here is your sunset over the ocean."},
			},
		},
	}

	store := &stubObjectStorage{}
	repo := &stubAttachmentRepo{}
	reg := tools.NewRegistry(10 * time.Second)
	reg.Register(tools.NewGenerateImageTool(tools.GenerateImageOptions{
		Provider:  &stubImageProvider{data: tinyPNG(t)},
		Storage:   store,
		RateLimit: rate.Limit(10),
		RateBurst: 5,
	}))

	svc := NewChatService(nil, repo, provider, ChatServiceConfig{ModelID: "gemini-3.1-flash-lite"}).
		WithToolRegistry(reg).
		WithAttachmentStore(repo, store)

	var statuses []ToolStatusEvent
	var attachments []domain.MessageAttachment
	var deltas []string

	_, assistantMsg, err := svc.StreamMessageWithEvents(context.Background(), ownerID, convID, "Generate a sunset ocean", StreamCallbacks{
		OnDelta: func(delta string) error {
			deltas = append(deltas, delta)
			return nil
		},
		OnToolStatus: func(event ToolStatusEvent) error {
			statuses = append(statuses, event)
			return nil
		},
		OnAttachment: func(att domain.MessageAttachment) error {
			attachments = append(attachments, att)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("StreamMessageWithEvents failed: %v", err)
	}

	if len(statuses) != 2 || statuses[0].Status != "running" || statuses[1].Status != "completed" {
		t.Fatalf("unexpected tool statuses: %+v", statuses)
	}
	if len(attachments) != 1 || attachments[0].URL == "" {
		t.Fatalf("expected 1 signed attachment event, got: %+v", attachments)
	}
	if len(store.uploaded) != 1 {
		t.Fatalf("expected exactly 1 uploaded image (max 1 per request), got %d", len(store.uploaded))
	}
	if len(provider.requests) != 2 {
		t.Fatalf("expected 2 provider turns (initial + follow-up), got %d", len(provider.requests))
	}
	if len(provider.requests[1].Tools) != 0 {
		t.Errorf("follow-up turn must not pass tools (prevents infinite loops)")
	}
	if assistantMsg.Content != "Here is your sunset over the ocean." {
		t.Errorf("unexpected assistant content: %q", assistantMsg.Content)
	}
	if len(assistantMsg.Attachments) != 1 {
		t.Errorf("expected 1 persisted attachment on assistant message")
	}
}

func TestChatService_ToolFailureAndOrphanCleanup(t *testing.T) {
	ownerID := "11111111-1111-1111-1111-111111111111"
	convID := "22222222-2222-2222-2222-222222222222"

	// Case 1: Provider rate-limited -> truthful failure status & message
	provider := &scriptedProvider{
		turns: [][]ai.StreamChunk{{
			{ToolCalls: []ai.ToolCall{{ID: "c1", Name: tools.ToolNameGenerateImage, Arguments: json.RawMessage(`{"prompt":"cat"}`)}}},
		}},
	}
	store := &stubObjectStorage{}
	repo := &stubAttachmentRepo{}
	reg := tools.NewRegistry(5 * time.Second)
	reg.Register(tools.NewGenerateImageTool(tools.GenerateImageOptions{
		Provider:  &stubImageProvider{err: aiimage.ErrProviderRateLimited},
		Storage:   store,
		RateLimit: rate.Limit(10),
		RateBurst: 5,
	}))

	svc := NewChatService(nil, repo, provider, ChatServiceConfig{}).
		WithToolRegistry(reg).
		WithAttachmentStore(repo, store)

	var statuses []ToolStatusEvent
	_, assistantMsg, err := svc.StreamMessageWithEvents(context.Background(), ownerID, convID, "Draw a cat", StreamCallbacks{
		OnToolStatus: func(event ToolStatusEvent) error {
			statuses = append(statuses, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected graceful fallback message, got error: %v", err)
	}
	if len(statuses) != 2 || statuses[1].Status != "failed" {
		t.Fatalf("expected running -> failed tool statuses, got %+v", statuses)
	}
	if !strings.Contains(strings.ToLower(assistantMsg.Content), "rate-limited") {
		t.Errorf("expected truthful rate-limit explanation, got %q", assistantMsg.Content)
	}

	// Case 2: DB persistence fails after upload -> uploaded storage object is deleted (no orphan)
	provider2 := &scriptedProvider{
		turns: [][]ai.StreamChunk{
			{{InteractionID: "i1", ToolCalls: []ai.ToolCall{{ID: "c2", Name: tools.ToolNameGenerateImage, Arguments: json.RawMessage(`{"prompt":"dog"}`)}}}},
			{{Delta: "Generated dog."}},
		},
	}
	store2 := &stubObjectStorage{}
	repo2 := &stubAttachmentRepo{failAssistantDB: true}
	reg2 := tools.NewRegistry(5 * time.Second)
	reg2.Register(tools.NewGenerateImageTool(tools.GenerateImageOptions{
		Provider:  &stubImageProvider{data: tinyPNG(t)},
		Storage:   store2,
		RateLimit: rate.Limit(10),
		RateBurst: 5,
	}))
	svc2 := NewChatService(nil, repo2, provider2, ChatServiceConfig{}).
		WithToolRegistry(reg2).
		WithAttachmentStore(repo2, store2)

	_, _, err = svc2.StreamMessageWithEvents(context.Background(), ownerID, convID, "Draw a dog", StreamCallbacks{})
	if !errors.Is(err, domain.ErrInternal) {
		t.Fatalf("expected ErrInternal on DB failure, got %v", err)
	}
	if len(store2.uploaded) != 1 || len(store2.deleted) != 1 || store2.uploaded[0] != store2.deleted[0] {
		t.Errorf("expected orphan cleanup on DB failure, uploaded=%v deleted=%v", store2.uploaded, store2.deleted)
	}
}

func TestChatService_PseudoToolJSONGuarded(t *testing.T) {
	ownerID := "11111111-1111-1111-1111-111111111111"
	convID := "22222222-2222-2222-2222-222222222222"

	provider := &scriptedProvider{
		turns: [][]ai.StreamChunk{{{Delta: `{"action":"dalle.text2im","action_input":"{\"prompt\":\"spaceship\"}"}`}}},
	}
	repo := &stubAttachmentRepo{}
	svc := NewChatService(nil, repo, provider, ChatServiceConfig{})

	_, assistantMsg, err := svc.StreamMessageWithEvents(context.Background(), ownerID, convID, "Draw a spaceship", StreamCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(assistantMsg.Content, "dalle.text2im") {
		t.Errorf("raw dalle.text2im JSON must never be persisted as assistant output: %q", assistantMsg.Content)
	}
}

func TestConversationService_DeleteCleansUpStorage(t *testing.T) {
	ownerID := "11111111-1111-1111-1111-111111111111"
	convID := "22222222-2222-2222-2222-222222222222"

	convRepo := &mockConvRepo{}
	attRepo := &stubAttachmentRepo{paths: []string{"owner/conv/img1.jpg", "owner/conv/img2.png"}}
	store := &stubObjectStorage{}

	svc := NewConversationService(convRepo).WithAttachmentCleanup(attRepo, store)
	if err := svc.Delete(context.Background(), ownerID, convID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if len(store.deleted) != 2 {
		t.Errorf("expected 2 storage objects deleted, got %v", store.deleted)
	}
}

func TestChatService_ToolFailureWithPreambleTextPersistsFailureExplanation(t *testing.T) {
	ownerID := "11111111-1111-1111-1111-111111111111"
	convID := "22222222-2222-2222-2222-222222222222"

	provider := &scriptedProvider{
		turns: [][]ai.StreamChunk{{
			{Delta: "Sure, I will generate that image for you."},
			{ToolCalls: []ai.ToolCall{{ID: "c-pre", Name: tools.ToolNameGenerateImage, Arguments: json.RawMessage(`{"prompt":"misty forest"}`)}}},
		}},
	}
	store := &stubObjectStorage{}
	repo := &stubAttachmentRepo{}
	reg := tools.NewRegistry(5 * time.Second)
	reg.Register(tools.NewGenerateImageTool(tools.GenerateImageOptions{
		Provider:  &stubImageProvider{err: aiimage.ErrProviderFailure},
		Storage:   store,
		RateLimit: rate.Limit(10),
		RateBurst: 5,
	}))

	svc := NewChatService(nil, repo, provider, ChatServiceConfig{}).
		WithToolRegistry(reg).
		WithAttachmentStore(repo, store)

	var streamed strings.Builder
	_, assistantMsg, err := svc.StreamMessageWithEvents(context.Background(), ownerID, convID, "Draw a misty forest", StreamCallbacks{
		OnDelta: func(delta string) error {
			streamed.WriteString(delta)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(assistantMsg.Content, "Sure, I will generate that image for you.") {
		t.Errorf("expected preamble text preserved, got: %q", assistantMsg.Content)
	}
	if !strings.Contains(assistantMsg.Content, "Image generation failed.") {
		t.Errorf("expected truthful failure explanation persisted alongside preamble, got: %q", assistantMsg.Content)
	}
	if streamed.String() != assistantMsg.Content {
		t.Errorf("streamed text %q did not match persisted content %q", streamed.String(), assistantMsg.Content)
	}
}

func TestChatService_SuccessfulImageGenStripsDallePseudoToolAndThought(t *testing.T) {
	ownerID := "11111111-1111-1111-1111-111111111111"
	convID := "22222222-2222-2222-2222-222222222222"

	// Simulate Gemini emitting raw dalle.text2im + thought JSON alongside a real generate_image call
	provider := &scriptedProvider{
		turns: [][]ai.StreamChunk{
			{
				{Delta: `{"action":"dalle.text2im","action_input":{"prompt":"minimalist sunlit studio desk"},"thought":"I should call image tool"}`},
				{
					InteractionID: "inter_leak_1",
					ToolCalls: []ai.ToolCall{
						{ID: "call_leak_1", Name: tools.ToolNameGenerateImage, Arguments: json.RawMessage(`{"prompt":"minimalist sunlit studio desk with warm morning shadows"}`)},
					},
				},
			},
			{
				{Delta: "Here is your minimalist sunlit studio desk with warm morning shadows."},
			},
		},
	}

	store := &stubObjectStorage{}
	repo := &stubAttachmentRepo{}
	reg := tools.NewRegistry(10 * time.Second)
	reg.Register(tools.NewGenerateImageTool(tools.GenerateImageOptions{
		Provider:  &stubImageProvider{data: tinyPNG(t)},
		Storage:   store,
		RateLimit: rate.Limit(10),
		RateBurst: 5,
	}))

	svc := NewChatService(nil, repo, provider, ChatServiceConfig{ModelID: "gemini-3.1-flash-lite"}).
		WithToolRegistry(reg).
		WithAttachmentStore(repo, store)

	var streamed strings.Builder
	var attachments []domain.MessageAttachment
	_, assistantMsg, err := svc.StreamMessageWithEvents(context.Background(), ownerID, convID, "Generate an image of a minimalist sunlit studio desk with warm morning shadows.", StreamCallbacks{
		OnDelta: func(delta string) error {
			streamed.WriteString(delta)
			return nil
		},
		OnAttachment: func(att domain.MessageAttachment) error {
			attachments = append(attachments, att)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("StreamMessageWithEvents failed: %v", err)
	}

	if len(attachments) != 1 || len(assistantMsg.Attachments) != 1 {
		t.Fatalf("expected generated attachment to be delivered and persisted")
	}
	for _, forbidden := range []string{"dalle.text2im", "action_input", "I should call image tool"} {
		if strings.Contains(streamed.String(), forbidden) {
			t.Errorf("SSE streamed text leaked %q: %q", forbidden, streamed.String())
		}
		if strings.Contains(assistantMsg.Content, forbidden) {
			t.Errorf("persisted assistant content leaked %q: %q", forbidden, assistantMsg.Content)
		}
	}
	if assistantMsg.Content != "Here is your minimalist sunlit studio desk with warm morning shadows." {
		t.Errorf("unexpected assistant content: %q", assistantMsg.Content)
	}
}

func TestChatService_PreservesLegitimateUserRequestedJSON(t *testing.T) {
	ownerID := "11111111-1111-1111-1111-111111111111"
	convID := "22222222-2222-2222-2222-222222222222"

	expected := "Here is a sample user JSON:\n\n```json\n{\"name\":\"Alice\",\"role\":\"admin\"}\n```"
	provider := &scriptedProvider{
		turns: [][]ai.StreamChunk{{
			{Delta: "Here is a sample user JSON:\n\n```json\n"},
			{Delta: `{"name":"Alice",`},
			{Delta: `"role":"admin"}` + "\n```"},
		}},
	}
	repo := &stubAttachmentRepo{}
	svc := NewChatService(nil, repo, provider, ChatServiceConfig{})

	var streamed strings.Builder
	_, assistantMsg, err := svc.StreamMessageWithEvents(context.Background(), ownerID, convID, "Give me a sample JSON object", StreamCallbacks{
		OnDelta: func(delta string) error {
			streamed.WriteString(delta)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if streamed.String() != expected {
		t.Errorf("legitimate JSON corrupted in stream:\nwant: %q\ngot:  %q", expected, streamed.String())
	}
	if assistantMsg.Content != expected {
		t.Errorf("legitimate JSON corrupted in persisted content:\nwant: %q\ngot:  %q", expected, assistantMsg.Content)
	}
}

type mockConvRepo struct{}

func (m *mockConvRepo) CreateConversation(ctx context.Context, ownerID, title string) (domain.Conversation, error) {
	return domain.Conversation{}, nil
}
func (m *mockConvRepo) GetConversationByID(ctx context.Context, ownerID, id string) (domain.Conversation, error) {
	return domain.Conversation{}, nil
}
func (m *mockConvRepo) ListConversations(ctx context.Context, ownerID string, page domain.Page) ([]domain.Conversation, error) {
	return nil, nil
}
func (m *mockConvRepo) UpdateConversationTitle(ctx context.Context, ownerID, id, title string) (domain.Conversation, error) {
	return domain.Conversation{}, nil
}
func (m *mockConvRepo) DeleteConversation(ctx context.Context, ownerID, id string) error {
	return nil
}
