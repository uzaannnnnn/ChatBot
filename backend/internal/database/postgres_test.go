package database

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeError(t *testing.T) {
	t.Parallel()

	rawURL := "postgresql://postgres.projref:p%40ss%2Fw0rd@aws-0-region.pooler.supabase.com:5432/postgres?sslmode=require"
	original := errors.New("failed to connect to `user=postgres.projref database=postgres`: " +
		"password p@ss/w0rd rejected; escaped p%40ss%2Fw0rd; url " + rawURL)

	got := SanitizeError(original, rawURL).Error()

	for _, secret := range []string{rawURL, "p@ss/w0rd", "p%40ss%2Fw0rd", "postgres.projref"} {
		if strings.Contains(got, secret) {
			t.Errorf("sanitized error still contains %q: %s", secret, got)
		}
	}
	if errors.Is(SanitizeError(original, rawURL), original) {
		t.Error("sanitized error must not wrap the original error")
	}
	if SanitizeError(nil, rawURL) != nil {
		t.Error("expected nil for nil error")
	}
}

func TestBuildPoolConfig(t *testing.T) {
	t.Parallel()

	if _, err := BuildPoolConfig(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}
