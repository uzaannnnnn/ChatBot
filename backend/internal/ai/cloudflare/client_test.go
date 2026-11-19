package cloudflare

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aiimage "github.com/uzaannnnnn/ChatBot/backend/internal/ai/image"
)

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 12, G: 120, B: 220, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func makeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, G: 80, B: 40, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestClient_Generate_Base64JPEGSuccess(t *testing.T) {
	jpgBytes := makeTestJPEG(t, 64, 48)
	encoded := base64.StdEncoding.EncodeToString(jpgBytes)

	var capturedPath, capturedAuth, capturedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":  map[string]string{"image": encoded},
			"success": true,
			"errors":  []any{},
		})
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		BaseURL:   server.URL,
		AccountID: "acct-12345",
		APIToken:  "cf-secret-token-xyz",
	})

	res, err := client.Generate(context.Background(), aiimage.GenerateRequest{
		Prompt: "A calm alpine lake at sunrise",
	})
	if err != nil {
		t.Fatalf("Generate unexpected error: %v", err)
	}

	expectedPath := "/accounts/acct-12345/ai/run/" + DefaultImageModel
	if capturedPath != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, capturedPath)
	}
	if capturedAuth != "Bearer cf-secret-token-xyz" {
		t.Errorf("expected Bearer auth header, got %q", capturedAuth)
	}
	if !strings.Contains(capturedBody, `"prompt":"A calm alpine lake at sunrise"`) {
		t.Errorf("unexpected request body: %s", capturedBody)
	}
	if res.MIMEType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %q", res.MIMEType)
	}
	if res.Width != 64 || res.Height != 48 {
		t.Errorf("expected 64x48, got %dx%d", res.Width, res.Height)
	}
	if !bytes.Equal(res.Data, jpgBytes) {
		t.Errorf("decoded image bytes mismatch")
	}
}

func TestClient_Generate_DataURIPrefixPNG(t *testing.T) {
	pngBytes := makeTestPNG(t, 32, 32)
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result":  map[string]string{"image": dataURI},
			"success": true,
		})
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		BaseURL:   server.URL,
		AccountID: "acct",
		APIToken:  "tok",
	})

	res, err := client.Generate(context.Background(), aiimage.GenerateRequest{Prompt: "pixel art cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.MIMEType != "image/png" || res.Width != 32 || res.Height != 32 {
		t.Errorf("unexpected result: mime=%q %dx%d", res.MIMEType, res.Width, res.Height)
	}
}

func TestClient_Generate_RawBinaryFallback(t *testing.T) {
	pngBytes := makeTestPNG(t, 16, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer server.Close()

	client := NewClient(ClientOptions{BaseURL: server.URL, AccountID: "acct", APIToken: "tok"})
	res, err := client.Generate(context.Background(), aiimage.GenerateRequest{Prompt: "minimal icon"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.MIMEType != "image/png" || res.Width != 16 || res.Height != 20 {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestClient_Generate_InvalidResponsesAndMIME(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     error
	}{
		{
			name:        "invalid json",
			contentType: "application/json",
			body:        "{not-json",
			wantErr:     aiimage.ErrProviderFailure,
		},
		{
			name:        "empty image field",
			contentType: "application/json",
			body:        `{"result":{"image":""},"success":true}`,
			wantErr:     aiimage.ErrProviderFailure,
		},
		{
			name:        "invalid base64",
			contentType: "application/json",
			body:        `{"result":{"image":"!!!not_base64!!!"},"success":true}`,
			wantErr:     aiimage.ErrProviderFailure,
		},
		{
			name:        "non-image bytes disguised in base64",
			contentType: "application/json",
			body:        `{"result":{"image":"` + base64.StdEncoding.EncodeToString([]byte("<html>not an image</html>")) + `"},"success":true}`,
			wantErr:     aiimage.ErrUnsupportedImageType,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewClient(ClientOptions{BaseURL: server.URL, AccountID: "acct", APIToken: "tok"})
			_, err := client.Generate(context.Background(), aiimage.GenerateRequest{Prompt: "a forest"})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestClient_Generate_ErrorHandlingAndRedaction(t *testing.T) {
	secretToken := "super-secret-cf-token-99999"
	secretAccount := "secret-account-id-88888"

	cases := []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
	}{
		{
			name:       "400 bad request",
			statusCode: http.StatusBadRequest,
			body:       `{"success":false,"errors":[{"code":1000,"message":"bad prompt for ` + secretAccount + `"}]}`,
			wantErr:    aiimage.ErrInvalidPrompt,
		},
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"success":false,"errors":[{"code":9109,"message":"Invalid token ` + secretToken + `"}]}`,
			wantErr:    aiimage.ErrProviderAuth,
		},
		{
			name:       "403 forbidden",
			statusCode: http.StatusForbidden,
			body:       `{"success":false,"errors":[{"code":9109,"message":"Forbidden"}]}`,
			wantErr:    aiimage.ErrProviderAuth,
		},
		{
			name:       "429 rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"success":false,"errors":[{"code":10000,"message":"Quota exceeded"}]}`,
			wantErr:    aiimage.ErrProviderRateLimited,
		},
		{
			name:       "502 server error",
			statusCode: http.StatusBadGateway,
			body:       `upstream error`,
			wantErr:    aiimage.ErrProviderFailure,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewClient(ClientOptions{
				BaseURL:   server.URL,
				AccountID: secretAccount,
				APIToken:  secretToken,
			})
			_, err := client.Generate(context.Background(), aiimage.GenerateRequest{Prompt: "sunset"})
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("expected %v, got %v", tc.wantErr, err)
			}
			if strings.Contains(err.Error(), secretToken) || strings.Contains(err.Error(), secretAccount) {
				t.Errorf("credentials leaked in error message: %v", err)
			}
		})
	}
}

func TestClient_Generate_TimeoutAndValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		BaseURL:   server.URL,
		AccountID: "acct",
		APIToken:  "tok",
		Timeout:   40 * time.Millisecond,
	})

	_, err := client.Generate(context.Background(), aiimage.GenerateRequest{Prompt: "slow"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}

	_, err = client.Generate(context.Background(), aiimage.GenerateRequest{Prompt: "   "})
	if !errors.Is(err, aiimage.ErrInvalidPrompt) {
		t.Errorf("expected ErrInvalidPrompt for blank prompt, got %v", err)
	}

	unconfigured := NewClient(ClientOptions{})
	_, err = unconfigured.Generate(context.Background(), aiimage.GenerateRequest{Prompt: "valid"})
	if !errors.Is(err, aiimage.ErrProviderUnavailable) {
		t.Errorf("expected ErrProviderUnavailable, got %v", err)
	}
}
