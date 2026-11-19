// Package image defines the image generation provider interface, normalized
// result types, and binary image validation utilities.
package image

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"unicode/utf8"
)

const (
	MaxPromptLength = 1000
	MaxImageBytes   = 15 << 20 // 15 MiB
)

var (
	ErrInvalidPrompt        = errors.New("image: prompt is empty or exceeds maximum length")
	ErrUnsupportedImageType = errors.New("image: unsupported or invalid image format")
	ErrImageTooLarge        = errors.New("image: generated image exceeds maximum allowed size")
	ErrProviderUnavailable  = errors.New("image: provider is not configured")
	ErrProviderAuth         = errors.New("image: provider authentication failed")
	ErrProviderRateLimited  = errors.New("image: provider rate limit or quota exceeded")
	ErrProviderFailure      = errors.New("image: provider request failed")
)

// GenerateRequest describes a validated text-to-image generation request.
type GenerateRequest struct {
	Prompt string
}

// GenerateResult holds validated binary image bytes and normalized metadata.
type GenerateResult struct {
	Data     []byte
	MIMEType string
	Width    int
	Height   int
	Prompt   string
	Model    string
}

// Provider defines the contract for backend text-to-image generation providers.
type Provider interface {
	Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error)
}

// ValidatePrompt checks that a prompt is non-empty, valid UTF-8, and within length limits.
func ValidatePrompt(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !utf8.ValidString(trimmed) {
		return "", ErrInvalidPrompt
	}
	if utf8.RuneCountInString(trimmed) > MaxPromptLength {
		return "", ErrInvalidPrompt
	}
	return trimmed, nil
}

// ValidateImageBytes verifies magic bytes, restricts output to JPEG/PNG/WebP,
// and extracts image width and height.
func ValidateImageBytes(data []byte) (string, int, int, error) {
	if len(data) < 12 {
		return "", 0, 0, ErrUnsupportedImageType
	}
	if len(data) > MaxImageBytes {
		return "", 0, 0, ErrImageTooLarge
	}

	mimeType := detectMagicMIME(data)
	if mimeType == "" {
		return "", 0, 0, ErrUnsupportedImageType
	}

	if mimeType == "image/webp" {
		w, h, err := parseWebPDimensions(data)
		if err != nil {
			return "", 0, 0, fmt.Errorf("%w: %v", ErrUnsupportedImageType, err)
		}
		return mimeType, w, h, nil
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return "", 0, 0, ErrUnsupportedImageType
	}
	switch format {
	case "jpeg":
		if mimeType != "image/jpeg" {
			return "", 0, 0, ErrUnsupportedImageType
		}
	case "png":
		if mimeType != "image/png" {
			return "", 0, 0, ErrUnsupportedImageType
		}
	default:
		return "", 0, 0, ErrUnsupportedImageType
	}

	return mimeType, cfg.Width, cfg.Height, nil
}

func detectMagicMIME(data []byte) string {
	// PNG: 89 50 4E 47 0D 0A 1A 0A
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png"
	}
	// JPEG: FF D8 FF
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	// WebP: RIFF....WEBP
	if len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")) {
		return "image/webp"
	}
	return ""
}

// parseWebPDimensions parses width and height from VP8, VP8L, or VP8X WebP chunks.
func parseWebPDimensions(data []byte) (int, int, error) {
	if len(data) < 30 {
		return 0, 0, errors.New("webp header too short")
	}
	chunk := string(data[12:16])
	switch chunk {
	case "VP8X":
		w := 1 + int(data[24]) | (int(data[25]) << 8) | (int(data[26]) << 16)
		h := 1 + int(data[27]) | (int(data[28]) << 8) | (int(data[29]) << 16)
		if w <= 0 || h <= 0 {
			return 0, 0, errors.New("invalid VP8X dimensions")
		}
		return w, h, nil
	case "VP8L":
		if data[20] != 0x2F {
			return 0, 0, errors.New("invalid VP8L signature")
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		w := int(bits&0x3FFF) + 1
		h := int((bits>>14)&0x3FFF) + 1
		return w, h, nil
	case "VP8 ":
		if data[23] != 0x9D || data[24] != 0x01 || data[25] != 0x2A {
			return 0, 0, errors.New("invalid VP8 keyframe signature")
		}
		w := int(binary.LittleEndian.Uint16(data[26:28]) & 0x3FFF)
		h := int(binary.LittleEndian.Uint16(data[28:30]) & 0x3FFF)
		if w <= 0 || h <= 0 {
			return 0, 0, errors.New("invalid VP8 dimensions")
		}
		return w, h, nil
	default:
		return 0, 0, fmt.Errorf("unsupported webp chunk %q", chunk)
	}
}
