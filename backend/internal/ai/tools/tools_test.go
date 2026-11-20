package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	aiimage "github.com/uzaannnnnn/ChatBot/backend/internal/ai/image"
	"golang.org/x/time/rate"
)

type fakeImageProvider struct {
	res aiimage.GenerateResult
	err error
}

func (f *fakeImageProvider) Generate(ctx context.Context, req aiimage.GenerateRequest) (aiimage.GenerateResult, error) {
	if f.err != nil {
		return aiimage.GenerateResult{}, f.err
	}
	r := f.res
	r.Prompt = req.Prompt
	return r, nil
}

type fakeStorage struct {
	uploaded  []string
	deleted   []string
	signErr   error
	uploadErr error
}

func (f *fakeStorage) UploadImage(ctx context.Context, objectPath string, data []byte, mimeType string) error {
	if f.uploadErr != nil {
		return f.uploadErr
	}
	f.uploaded = append(f.uploaded, objectPath)
	return nil
}

func (f *fakeStorage) CreateSignedURL(ctx context.Context, objectPath string, ttl time.Duration) (string, error) {
	if f.signErr != nil {
		return "", f.signErr
	}
	return "https://storage.example/signed/" + objectPath, nil
}

func (f *fakeStorage) DeleteObjects(ctx context.Context, objectPaths []string) error {
	f.deleted = append(f.deleted, objectPaths...)
	return nil
}

func samplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestGenerateImageTool_SuccessAndUsageTracking(t *testing.T) {
	store := &fakeStorage{}
	provider := &fakeImageProvider{
		res: aiimage.GenerateResult{
			Data:     samplePNG(t),
			MIMEType: "image/png",
			Width:    16,
			Height:   16,
			Model:    "@cf/black-forest-labs/flux-1-schnell",
		},
	}

	reg := NewRegistry(10 * time.Second)
	reg.Register(NewGenerateImageTool(GenerateImageOptions{
		Provider:  provider,
		Storage:   store,
		RateLimit: rate.Limit(10),
		RateBurst: 5,
	}))

	execCtx := ExecutionContext{
		OwnerID:        "11111111-1111-1111-1111-111111111111",
		ConversationID: "22222222-2222-2222-2222-222222222222",
	}

	res, err := reg.Execute(context.Background(), execCtx, ai.ToolCall{
		ID:        "call_1",
		Name:      ToolNameGenerateImage,
		Arguments: json.RawMessage(`{"prompt":"A futuristic lighthouse"}`),
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	if res.Attachment == nil {
		t.Fatalf("expected attachment result")
	}
	if len(store.uploaded) != 1 {
		t.Fatalf("expected 1 uploaded object, got %d", len(store.uploaded))
	}
	if res.Attachment.Attachment.URL == "" {
		t.Errorf("expected signed URL")
	}
	if reg.Usage().Successes != 1 || reg.Usage().Invocations != 1 || reg.Usage().Failures != 0 {
		t.Errorf("unexpected usage metrics: %+v", reg.Usage())
	}
}

func TestGenerateImageTool_StrictValidationAndRateLimit(t *testing.T) {
	store := &fakeStorage{}
	provider := &fakeImageProvider{
		res: aiimage.GenerateResult{Data: samplePNG(t), MIMEType: "image/png", Width: 16, Height: 16},
	}
	tool := NewGenerateImageTool(GenerateImageOptions{
		Provider:  provider,
		Storage:   store,
		RateLimit: rate.Limit(0.01),
		RateBurst: 1,
	})
	reg := NewRegistry(5 * time.Second)
	reg.Register(tool)

	execCtx := ExecutionContext{OwnerID: "owner-1", ConversationID: "conv-1"}

	// Reject unknown fields
	_, err := reg.Execute(context.Background(), execCtx, ai.ToolCall{
		Name:      ToolNameGenerateImage,
		Arguments: json.RawMessage(`{"prompt":"cat","unknown_param":123}`),
	})
	if !errors.Is(err, ErrInvalidToolInput) {
		t.Errorf("expected ErrInvalidToolInput for unknown field, got %v", err)
	}

	// First valid call succeeds
	if _, err := reg.Execute(context.Background(), execCtx, ai.ToolCall{
		Name:      ToolNameGenerateImage,
		Arguments: json.RawMessage(`{"prompt":"cat"}`),
	}); err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Second immediate call hits per-user rate limit
	_, err = reg.Execute(context.Background(), execCtx, ai.ToolCall{
		Name:      ToolNameGenerateImage,
		Arguments: json.RawMessage(`{"prompt":"dog"}`),
	})
	if !errors.Is(err, ErrToolRateLimited) {
		t.Errorf("expected ErrToolRateLimited, got %v", err)
	}
}

func TestGenerateImageTool_OrphanCleanupOnSignFailure(t *testing.T) {
	store := &fakeStorage{signErr: errors.New("sign failure")}
	provider := &fakeImageProvider{
		res: aiimage.GenerateResult{Data: samplePNG(t), MIMEType: "image/png", Width: 16, Height: 16},
	}
	reg := NewRegistry(5 * time.Second)
	reg.Register(NewGenerateImageTool(GenerateImageOptions{
		Provider:  provider,
		Storage:   store,
		RateLimit: rate.Limit(10),
		RateBurst: 5,
	}))

	_, err := reg.Execute(context.Background(), ExecutionContext{OwnerID: "u1", ConversationID: "c1"}, ai.ToolCall{
		Name:      ToolNameGenerateImage,
		Arguments: json.RawMessage(`{"prompt":"mountains"}`),
	})
	if err == nil {
		t.Fatalf("expected sign failure error")
	}
	if len(store.uploaded) != 1 || len(store.deleted) != 1 || store.uploaded[0] != store.deleted[0] {
		t.Errorf("expected uploaded orphan to be deleted, uploaded=%v deleted=%v", store.uploaded, store.deleted)
	}
}

func TestBuildAltText_PreservesFullPromptAndUTF8(t *testing.T) {
	longPrompt := "A minimalist studio desk bathed in soft, warm morning sunlight. The scene features long, elegant shadows cast across a clean, light-colored wooden desk surface with a ceramic mug and notebook."
	if got := buildAltText("  " + longPrompt + "  "); got != longPrompt {
		t.Errorf("expected full >160-char prompt preserved:\nwant: %q\ngot:  %q", longPrompt, got)
	}

	utf8Prompt := "日出時の静かな海と温かい朝の光 — café minimalist studio desk with warm morning shadows 🌅✨ " +
		"A serene sunlit workspace with clean wooden grain, soft natural shadows, and peaceful atmosphere extending well beyond one hundred and sixty bytes."
	gotUTF8 := buildAltText(utf8Prompt)
	if gotUTF8 != utf8Prompt {
		t.Errorf("expected multi-byte UTF-8 prompt preserved verbatim:\nwant: %q\ngot:  %q", utf8Prompt, gotUTF8)
	}

	overLimit := ""
	for i := 0; i < aiimage.MaxPromptLength+25; i++ {
		overLimit += "あ"
	}
	bounded := buildAltText(overLimit)
	if len([]rune(bounded)) != aiimage.MaxPromptLength {
		t.Errorf("expected prompt bounded to %d runes, got %d", aiimage.MaxPromptLength, len([]rune(bounded)))
	}
}
