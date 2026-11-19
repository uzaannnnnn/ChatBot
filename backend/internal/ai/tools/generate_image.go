package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/uzaannnnnn/ChatBot/backend/internal/ai"
	aiimage "github.com/uzaannnnnn/ChatBot/backend/internal/ai/image"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
	"github.com/uzaannnnnn/ChatBot/backend/internal/storage"
	"golang.org/x/time/rate"
)

const (
	ToolNameGenerateImage = "generate_image"

	// Conservative default per-user image generation limit: 1 image per 15s, burst of 3.
	DefaultImageRateLimit = rate.Limit(1.0 / 15.0)
	DefaultImageBurst     = 3
)

// GenerateImageOptions configures the generate_image backend tool.
type GenerateImageOptions struct {
	Provider     aiimage.Provider
	Storage      storage.ObjectStorage
	SignedURLTTL time.Duration
	RateLimit    rate.Limit
	RateBurst    int
}

type userLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// GenerateImageTool implements the generate_image tool using an image provider and private storage.
type GenerateImageTool struct {
	provider     aiimage.Provider
	storage      storage.ObjectStorage
	signedURLTTL time.Duration
	rateLimit    rate.Limit
	rateBurst    int

	mu       sync.Mutex
	limiters map[string]*userLimiterEntry
}

var _ Tool = (*GenerateImageTool)(nil)

// NewGenerateImageTool constructs a generate_image tool handler.
func NewGenerateImageTool(opts GenerateImageOptions) *GenerateImageTool {
	ttl := opts.SignedURLTTL
	if ttl <= 0 {
		ttl = storage.DefaultSignedURLTTL
	}
	rl := opts.RateLimit
	if rl <= 0 {
		rl = DefaultImageRateLimit
	}
	burst := opts.RateBurst
	if burst <= 0 {
		burst = DefaultImageBurst
	}
	return &GenerateImageTool{
		provider:     opts.Provider,
		storage:      opts.Storage,
		signedURLTTL: ttl,
		rateLimit:    rl,
		rateBurst:    burst,
		limiters:     make(map[string]*userLimiterEntry),
	}
}

func (t *GenerateImageTool) Name() string {
	return ToolNameGenerateImage
}

func (t *GenerateImageTool) Definition() ai.ToolDefinition {
	return ai.ToolDefinition{
		Name:        ToolNameGenerateImage,
		Description: "Generate a single original image from a detailed text prompt when the user asks to create, draw, illustrate, or visualize an image.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "A clear, vivid description of the image to generate (max 1000 characters).",
				},
			},
			"required": []string{"prompt"},
		},
	}
}

type generateImageInput struct {
	Prompt string `json:"prompt"`
}

func (t *GenerateImageTool) allowUser(ownerID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	if len(t.limiters) > 1000 {
		for k, entry := range t.limiters {
			if now.Sub(entry.lastSeen) > 15*time.Minute {
				delete(t.limiters, k)
			}
		}
	}

	entry, exists := t.limiters[ownerID]
	if !exists {
		entry = &userLimiterEntry{
			limiter: rate.NewLimiter(t.rateLimit, t.rateBurst),
		}
		t.limiters[ownerID] = entry
	}
	entry.lastSeen = now
	return entry.limiter.Allow()
}

func (t *GenerateImageTool) Execute(ctx context.Context, execCtx ExecutionContext, rawArgs json.RawMessage) (ExecutionResult, error) {
	if t.provider == nil || t.storage == nil {
		return ExecutionResult{}, aiimage.ErrProviderUnavailable
	}

	// Strict JSON decoding: reject unknown fields and trailing data
	var input generateImageInput
	dec := json.NewDecoder(bytes.NewReader(rawArgs))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		return ExecutionResult{}, fmt.Errorf("%w: %v", ErrInvalidToolInput, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return ExecutionResult{}, fmt.Errorf("%w: unexpected trailing JSON", ErrInvalidToolInput)
	}

	prompt, err := aiimage.ValidatePrompt(input.Prompt)
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("%w: %v", ErrInvalidToolInput, err)
	}

	if !t.allowUser(execCtx.OwnerID) {
		return ExecutionResult{}, ErrToolRateLimited
	}

	generated, err := t.provider.Generate(ctx, aiimage.GenerateRequest{Prompt: prompt})
	if err != nil {
		return ExecutionResult{}, err
	}

	attachmentID, err := newUUIDv4()
	if err != nil {
		return ExecutionResult{}, fmt.Errorf("generate attachment id: %w", err)
	}

	objectPath := storage.BuildAttachmentObjectPath(
		execCtx.OwnerID,
		execCtx.ConversationID,
		attachmentID,
		generated.MIMEType,
	)

	if err := t.storage.UploadImage(ctx, objectPath, generated.Data, generated.MIMEType); err != nil {
		return ExecutionResult{}, err
	}

	signedURL, err := t.storage.CreateSignedURL(ctx, objectPath, t.signedURLTTL)
	if err != nil {
		// Clean up uploaded object so we do not leave an orphan when signing fails
		_ = t.storage.DeleteObjects(context.Background(), []string{objectPath})
		return ExecutionResult{}, err
	}

	altText := buildAltText(prompt)
	newRecord := domain.NewMessageAttachment{
		ID:          attachmentID,
		Kind:        domain.AttachmentKindImage,
		StoragePath: objectPath,
		MIMEType:    generated.MIMEType,
		Width:       generated.Width,
		Height:      generated.Height,
		Prompt:      prompt,
		AltText:     altText,
	}
	att := domain.MessageAttachment{
		ID:          attachmentID,
		Kind:        domain.AttachmentKindImage,
		StoragePath: objectPath,
		URL:         signedURL,
		MIMEType:    generated.MIMEType,
		Width:       generated.Width,
		Height:      generated.Height,
		Prompt:      prompt,
		AltText:     altText,
		CreatedAt:   time.Now().UTC(),
	}

	return ExecutionResult{
		ModelOutput: map[string]any{
			"status":        "generated",
			"attachment_id": attachmentID,
			"mime_type":     generated.MIMEType,
			"width":         generated.Width,
			"height":        generated.Height,
			"prompt":        prompt,
		},
		Attachment: &GeneratedAttachment{
			NewRecord:  newRecord,
			Attachment: att,
		},
	}, nil
}

func buildAltText(prompt string) string {
	cleaned := strings.TrimSpace(prompt)
	if len(cleaned) > 160 {
		return cleaned[:157] + "..."
	}
	return cleaned
}

func newUUIDv4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// UserFacingToolError converts internal tool errors into safe, truthful messages for users.
func UserFacingToolError(err error) string {
	switch {
	case errors.Is(err, ErrToolRateLimited), errors.Is(err, aiimage.ErrProviderRateLimited):
		return "Image generation is temporarily rate-limited or quota-exhausted. Please wait a moment and try again."
	case errors.Is(err, aiimage.ErrProviderUnavailable), errors.Is(err, storage.ErrNotConfigured):
		return "Image generation is not configured on this server right now."
	case errors.Is(err, aiimage.ErrProviderAuth):
		return "Image generation failed due to a backend provider authentication error."
	case errors.Is(err, ErrInvalidToolInput), errors.Is(err, aiimage.ErrInvalidPrompt):
		return "Image generation could not run because the image prompt was invalid."
	case errors.Is(err, context.DeadlineExceeded):
		return "Image generation timed out. Please try again with a simpler prompt."
	default:
		return "Image generation failed. Please try again."
	}
}
