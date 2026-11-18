package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSupabaseVerifier_JWKS_ES256(t *testing.T) {
	t.Parallel()

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}

	const kid = "test-ec-key-1"
	jwksPayload := jwksResponse{
		Keys: []jwkKey{
			{
				Kty: "EC",
				Kid: kid,
				Alg: "ES256",
				Use: "sig",
				Crv: "P-256",
				X:   base64.RawURLEncoding.EncodeToString(privKey.PublicKey.X.Bytes()),
				Y:   base64.RawURLEncoding.EncodeToString(privKey.PublicKey.Y.Bytes()),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(jwksPayload)
	}))
	defer server.Close()

	verifier := NewSupabaseVerifier(SupabaseVerifierOptions{
		SupabaseURL: server.URL,
	})

	sign := func(claims jwt.RegisteredClaims, signingKey *ecdsa.PrivateKey) string {
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		tok.Header["kid"] = kid
		s, err := tok.SignedString(signingKey)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		return s
	}

	validSub := "123e4567-e89b-12d3-a456-426614174000"
	validIssuer := server.URL + "/auth/v1"

	t.Run("valid token returns user UUID", func(t *testing.T) {
		tok := sign(jwt.RegisteredClaims{
			Subject:   validSub,
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
		}, privKey)

		sub, err := verifier.VerifyToken(context.Background(), tok)
		if err != nil {
			t.Fatalf("expected valid token, got %v", err)
		}
		if sub != validSub {
			t.Errorf("expected sub %q, got %q", validSub, sub)
		}
	})

	t.Run("expired token rejected", func(t *testing.T) {
		tok := sign(jwt.RegisteredClaims{
			Subject:   validSub,
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
		}, privKey)

		_, err := verifier.VerifyToken(context.Background(), tok)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken, got %v", err)
		}
	})

	t.Run("wrong signature rejected", func(t *testing.T) {
		otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tok := sign(jwt.RegisteredClaims{
			Subject:   validSub,
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		}, otherKey)

		_, err := verifier.VerifyToken(context.Background(), tok)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken, got %v", err)
		}
	})

	t.Run("wrong issuer rejected", func(t *testing.T) {
		tok := sign(jwt.RegisteredClaims{
			Subject:   validSub,
			Issuer:    "https://evil.example.com/auth/v1",
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		}, privKey)

		_, err := verifier.VerifyToken(context.Background(), tok)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken, got %v", err)
		}
	})

	t.Run("wrong audience rejected", func(t *testing.T) {
		tok := sign(jwt.RegisteredClaims{
			Subject:   validSub,
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"anon"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		}, privKey)

		_, err := verifier.VerifyToken(context.Background(), tok)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken, got %v", err)
		}
	})

	t.Run("non-uuid sub rejected", func(t *testing.T) {
		tok := sign(jwt.RegisteredClaims{
			Subject:   "not-a-uuid",
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		}, privKey)

		_, err := verifier.VerifyToken(context.Background(), tok)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken, got %v", err)
		}
	})

	t.Run("missing token rejected", func(t *testing.T) {
		_, err := verifier.VerifyToken(context.Background(), "")
		if !errors.Is(err, ErrMissingToken) {
			t.Errorf("expected ErrMissingToken, got %v", err)
		}
	})

	t.Run("missing sub rejected", func(t *testing.T) {
		tok := sign(jwt.RegisteredClaims{
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		}, privKey)
		_, err := verifier.VerifyToken(context.Background(), tok)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken for missing sub, got %v", err)
		}
	})

	t.Run("unknown kid rejected", func(t *testing.T) {
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
			Subject:   validSub,
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		})
		tok.Header["kid"] = "unknown-kid-999"
		signed, _ := tok.SignedString(privKey)
		_, err := verifier.VerifyToken(context.Background(), signed)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken for unknown kid, got %v", err)
		}
	})

	t.Run("unsupported alg none rejected", func(t *testing.T) {
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.RegisteredClaims{
			Subject:   validSub,
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		})
		signed, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		_, err := verifier.VerifyToken(context.Background(), signed)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken for alg=none, got %v", err)
		}
	})

	t.Run("HS256 rejected when secret not explicitly configured", func(t *testing.T) {
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
			Subject:   validSub,
			Issuer:    validIssuer,
			Audience:  jwt.ClaimStrings{"authenticated"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		})
		signed, _ := tok.SignedString([]byte("arbitrary-secret"))
		_, err := verifier.VerifyToken(context.Background(), signed)
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expected ErrInvalidToken for HS256 without secret, got %v", err)
		}
	})
}

func TestSupabaseVerifier_HS256(t *testing.T) {
	t.Parallel()

	secret := "super-secret-supabase-jwt-signing-key"
	verifier := NewSupabaseVerifier(SupabaseVerifierOptions{
		JWTSecret: secret,
	})

	validSub := "123e4567-e89b-12d3-a456-426614174000"
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   validSub,
		Audience:  jwt.ClaimStrings{"authenticated"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
	})
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign HS256: %v", err)
	}

	sub, err := verifier.VerifyToken(context.Background(), signed)
	if err != nil || sub != validSub {
		t.Fatalf("expected valid HS256 token for %s, got sub=%s err=%v", validSub, sub, err)
	}
}
