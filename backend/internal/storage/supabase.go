// Package storage provides backend-controlled private object storage
// for generated images and message attachments using Supabase Storage.
package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	aiimage "github.com/uzaannnnnn/ChatBot/backend/internal/ai/image"
)

const (
	DefaultBucket       = "chat-attachments"
	DefaultSignedURLTTL = 15 * time.Minute
	DefaultTimeout      = 30 * time.Second
)

var (
	ErrNotConfigured = errors.New("storage: supabase storage is not configured")
	ErrInvalidPath   = errors.New("storage: invalid object storage path")
	ErrUploadFailed  = errors.New("storage: failed to upload object")
	ErrSignFailed    = errors.New("storage: failed to create signed URL")
	ErrDeleteFailed  = errors.New("storage: failed to delete objects")
)

// ObjectStorage defines backend object storage operations for private attachments.
type ObjectStorage interface {
	UploadImage(ctx context.Context, objectPath string, data []byte, mimeType string) error
	CreateSignedURL(ctx context.Context, objectPath string, ttl time.Duration) (string, error)
	DeleteObjects(ctx context.Context, objectPaths []string) error
}

// SupabaseOptions configures the Supabase Storage REST client.
type SupabaseOptions struct {
	SupabaseURL    string
	ServiceRoleKey string
	Bucket         string
	Timeout        time.Duration
	HTTPClient     *http.Client
}

// SupabaseClient implements ObjectStorage via the Supabase Storage REST API.
type SupabaseClient struct {
	baseURL        string
	serviceRoleKey string
	bucket         string
	httpClient     *http.Client
}

var _ ObjectStorage = (*SupabaseClient)(nil)

// NewSupabaseClient creates a new Supabase Storage client.
func NewSupabaseClient(opts SupabaseOptions) *SupabaseClient {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.SupabaseURL), "/")
	bucket := strings.TrimSpace(opts.Bucket)
	if bucket == "" {
		bucket = DefaultBucket
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &SupabaseClient{
		baseURL:        baseURL,
		serviceRoleKey: strings.TrimSpace(opts.ServiceRoleKey),
		bucket:         bucket,
		httpClient:     httpClient,
	}
}

// UploadImage validates the image bytes and uploads them to the private Supabase bucket.
func (c *SupabaseClient) UploadImage(ctx context.Context, objectPath string, data []byte, mimeType string) error {
	if c.baseURL == "" || c.serviceRoleKey == "" {
		return ErrNotConfigured
	}
	cleanPath, err := sanitizeObjectPath(objectPath)
	if err != nil {
		return err
	}

	detectedMIME, _, _, err := aiimage.ValidateImageBytes(data)
	if err != nil {
		return err
	}
	if mimeType != "" && mimeType != detectedMIME {
		return aiimage.ErrUnsupportedImageType
	}

	endpoint := fmt.Sprintf("%s/storage/v1/object/%s/%s", c.baseURL, url.PathEscape(c.bucket), encodePathSegments(cleanPath))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return c.redactError(fmt.Errorf("%w: %v", ErrUploadFailed, err))
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
	req.Header.Set("apikey", c.serviceRoleKey)
	req.Header.Set("Content-Type", detectedMIME)
	req.Header.Set("x-upsert", "false")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.redactError(fmt.Errorf("%w: %v", ErrUploadFailed, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return c.redactError(fmt.Errorf("%w: HTTP %d: %s", ErrUploadFailed, resp.StatusCode, strings.TrimSpace(string(body))))
	}
	return nil
}

// CreateSignedURL generates a short-lived signed URL for an object in the private bucket.
func (c *SupabaseClient) CreateSignedURL(ctx context.Context, objectPath string, ttl time.Duration) (string, error) {
	if c.baseURL == "" || c.serviceRoleKey == "" {
		return "", ErrNotConfigured
	}
	cleanPath, err := sanitizeObjectPath(objectPath)
	if err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = DefaultSignedURLTTL
	}
	expiresIn := int(ttl.Seconds())
	if expiresIn < 60 {
		expiresIn = 60
	}

	endpoint := fmt.Sprintf("%s/storage/v1/object/sign/%s/%s", c.baseURL, url.PathEscape(c.bucket), encodePathSegments(cleanPath))
	payload, _ := json.Marshal(map[string]int{"expiresIn": expiresIn})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", c.redactError(fmt.Errorf("%w: %v", ErrSignFailed, err))
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
	req.Header.Set("apikey", c.serviceRoleKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", c.redactError(fmt.Errorf("%w: %v", ErrSignFailed, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", c.redactError(fmt.Errorf("%w: HTTP %d: %s", ErrSignFailed, resp.StatusCode, strings.TrimSpace(string(body))))
	}

	var result struct {
		SignedURL string `json:"signedURL"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", c.redactError(fmt.Errorf("%w: decode response: %v", ErrSignFailed, err))
	}
	signed := strings.TrimSpace(result.SignedURL)
	if signed == "" {
		return "", fmt.Errorf("%w: empty signed URL", ErrSignFailed)
	}
	if strings.HasPrefix(signed, "http://") || strings.HasPrefix(signed, "https://") {
		return signed, nil
	}
	if strings.HasPrefix(signed, "/storage/v1/") {
		return c.baseURL + signed, nil
	}
	return c.baseURL + "/storage/v1" + signed, nil
}

// DeleteObjects removes one or more objects from the private bucket.
func (c *SupabaseClient) DeleteObjects(ctx context.Context, objectPaths []string) error {
	if len(objectPaths) == 0 {
		return nil
	}
	if c.baseURL == "" || c.serviceRoleKey == "" {
		return ErrNotConfigured
	}

	cleaned := make([]string, 0, len(objectPaths))
	for _, p := range objectPaths {
		cp, err := sanitizeObjectPath(p)
		if err != nil {
			continue
		}
		cleaned = append(cleaned, cp)
	}
	if len(cleaned) == 0 {
		return nil
	}

	endpoint := fmt.Sprintf("%s/storage/v1/object/%s", c.baseURL, url.PathEscape(c.bucket))
	payload, _ := json.Marshal(map[string][]string{"prefixes": cleaned})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(payload))
	if err != nil {
		return c.redactError(fmt.Errorf("%w: %v", ErrDeleteFailed, err))
	}
	req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
	req.Header.Set("apikey", c.serviceRoleKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.redactError(fmt.Errorf("%w: %v", ErrDeleteFailed, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return c.redactError(fmt.Errorf("%w: HTTP %d: %s", ErrDeleteFailed, resp.StatusCode, strings.TrimSpace(string(body))))
	}
	return nil
}

// BuildAttachmentObjectPath constructs a deterministic, owner-scoped object path.
func BuildAttachmentObjectPath(ownerID, conversationID, attachmentID, mimeType string) string {
	ext := ".jpg"
	switch mimeType {
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/jpeg":
		ext = ".jpg"
	}
	return fmt.Sprintf("%s/%s/%s%s", ownerID, conversationID, attachmentID, ext)
}

func sanitizeObjectPath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.Contains(trimmed, "..") || strings.HasPrefix(trimmed, "/") || strings.ContainsRune(trimmed, '\\') {
		return "", ErrInvalidPath
	}
	clean := path.Clean(trimmed)
	if clean == "." || clean == "" || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "..") {
		return "", ErrInvalidPath
	}
	return clean, nil
}

func encodePathSegments(cleanPath string) string {
	parts := strings.Split(cleanPath, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func (c *SupabaseClient) redactError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if c.serviceRoleKey != "" {
		msg = strings.ReplaceAll(msg, c.serviceRoleKey, "[REDACTED]")
	}
	return errors.New(msg)
}
