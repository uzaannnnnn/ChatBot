package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aiimage "github.com/uzaannnnnn/ChatBot/backend/internal/ai/image"
)

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestSupabaseClient_UploadSignDelete(t *testing.T) {
	var uploadedPath, signedPath string
	var deletedPrefixes []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer srv-secret-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/storage/v1/object/sign/"):
			signedPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]string{
				"signedURL": "/object/sign/chat-attachments/u/c/a.png?token=abc",
			})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/storage/v1/object/"):
			uploadedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Key":"chat-attachments/u/c/a.png"}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/v1/object/"):
			var body struct {
				Prefixes []string `json:"prefixes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			deletedPrefixes = body.Prefixes
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewSupabaseClient(SupabaseOptions{
		SupabaseURL:    server.URL,
		ServiceRoleKey: "srv-secret-key",
		Bucket:         "chat-attachments",
	})

	pngData := testPNGBytes(t)
	if err := client.UploadImage(context.Background(), "u/c/a.png", pngData, "image/png"); err != nil {
		t.Fatalf("UploadImage failed: %v", err)
	}
	if uploadedPath != "/storage/v1/object/chat-attachments/u/c/a.png" {
		t.Errorf("unexpected upload path: %q", uploadedPath)
	}

	signed, err := client.CreateSignedURL(context.Background(), "u/c/a.png", 10*time.Minute)
	if err != nil {
		t.Fatalf("CreateSignedURL failed: %v", err)
	}
	if !strings.HasPrefix(signed, server.URL+"/storage/v1/object/sign/") {
		t.Errorf("unexpected signed URL: %q", signed)
	}
	if !strings.Contains(signedPath, "/storage/v1/object/sign/chat-attachments/u/c/a.png") {
		t.Errorf("unexpected sign path: %q", signedPath)
	}

	if err := client.DeleteObjects(context.Background(), []string{"u/c/a.png"}); err != nil {
		t.Fatalf("DeleteObjects failed: %v", err)
	}
	if len(deletedPrefixes) != 1 || deletedPrefixes[0] != "u/c/a.png" {
		t.Errorf("unexpected deleted prefixes: %+v", deletedPrefixes)
	}
}

func TestSupabaseClient_ValidationAndRedaction(t *testing.T) {
	secret := "service-role-secret-value-123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("denied for " + secret))
	}))
	defer server.Close()

	client := NewSupabaseClient(SupabaseOptions{
		SupabaseURL:    server.URL,
		ServiceRoleKey: secret,
	})

	// Path traversal rejected
	if err := client.UploadImage(context.Background(), "../escape.png", testPNGBytes(t), "image/png"); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("expected ErrInvalidPath, got %v", err)
	}

	// Non-image bytes rejected
	if err := client.UploadImage(context.Background(), "u/c/bad.png", []byte("not-a-real-image-payload"), "image/png"); !errors.Is(err, aiimage.ErrUnsupportedImageType) {
		t.Errorf("expected ErrUnsupportedImageType, got %v", err)
	}

	// Error redacts service role key
	err := client.UploadImage(context.Background(), "u/c/ok.png", testPNGBytes(t), "image/png")
	if err == nil {
		t.Fatalf("expected error on 403")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("service role key leaked in error: %v", err)
	}
}
