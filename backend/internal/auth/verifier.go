// Package auth implements Supabase JWT verification using JWKS.
package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/uzaannnnnn/ChatBot/backend/internal/domain"
)

var (
	ErrMissingToken = errors.New("auth: missing bearer token")
	ErrInvalidToken = errors.New("auth: invalid or expired token")
)

// Verifier validates an access token and returns the authenticated user's UUID (sub).
type Verifier interface {
	VerifyToken(ctx context.Context, tokenString string) (string, error)
}

// SupabaseVerifierOptions configures Supabase JWT verification.
type SupabaseVerifierOptions struct {
	SupabaseURL string
	JWKSURL     string
	JWTSecret   string
	Audience    string
	HTTPClient  *http.Client
	CacheTTL    time.Duration
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// SupabaseVerifier verifies Supabase Auth JWTs against JWKS and/or an optional HS256 secret.
type SupabaseVerifier struct {
	issuer     string
	jwksURL    string
	jwtSecret  []byte
	audience   string
	httpClient *http.Client
	cacheTTL   time.Duration

	mu          sync.RWMutex
	keys        map[string]any
	lastFetched time.Time
}

var _ Verifier = (*SupabaseVerifier)(nil)

// NewSupabaseVerifier constructs a SupabaseVerifier.
func NewSupabaseVerifier(opts SupabaseVerifierOptions) *SupabaseVerifier {
	base := strings.TrimRight(strings.TrimSpace(opts.SupabaseURL), "/")
	jwksURL := strings.TrimSpace(opts.JWKSURL)
	if jwksURL == "" && base != "" {
		jwksURL = base + "/auth/v1/.well-known/jwks.json"
	}

	issuer := ""
	if base != "" {
		issuer = base + "/auth/v1"
	}

	audience := strings.TrimSpace(opts.Audience)
	if audience == "" {
		audience = "authenticated"
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	var secret []byte
	if strings.TrimSpace(opts.JWTSecret) != "" {
		secret = []byte(strings.TrimSpace(opts.JWTSecret))
	}

	return &SupabaseVerifier{
		issuer:     issuer,
		jwksURL:    jwksURL,
		jwtSecret:  secret,
		audience:   audience,
		httpClient: client,
		cacheTTL:   ttl,
		keys:       make(map[string]any),
	}
}

// VerifyToken validates signature, issuer, audience, expiration, and subject UUID.
func (v *SupabaseVerifier) VerifyToken(ctx context.Context, tokenString string) (string, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return "", ErrMissingToken
	}

	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"ES256", "ES384", "ES512", "RS256", "RS384", "RS512", "HS256"}),
		jwt.WithExpirationRequired(),
		jwt.WithAudience(v.audience),
	}
	if v.issuer != "" {
		parserOpts = append(parserOpts, jwt.WithIssuer(v.issuer))
	}

	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() == "HS256" {
			if len(v.jwtSecret) == 0 {
				return nil, fmt.Errorf("%w: HS256 secret not configured", ErrInvalidToken)
			}
			return v.jwtSecret, nil
		}

		kid, _ := t.Header["kid"].(string)
		return v.getKey(ctx, kid)
	}, parserOpts...)

	if err != nil || !token.Valid {
		return "", ErrInvalidToken
	}

	sub := strings.TrimSpace(claims.Subject)
	if !domain.IsValidUUID(sub) {
		return "", ErrInvalidToken
	}
	return sub, nil
}

func (v *SupabaseVerifier) getKey(ctx context.Context, kid string) (any, error) {
	v.mu.RLock()
	expired := time.Since(v.lastFetched) > v.cacheTTL
	key, ok := v.keys[kid]
	v.mu.RUnlock()

	if ok && !expired {
		return key, nil
	}

	if err := v.refreshJWKS(ctx); err != nil {
		// Fall back to cached key if present and within grace window
		if ok {
			return key, nil
		}
		return nil, err
	}

	v.mu.RLock()
	key, ok = v.keys[kid]
	if !ok && kid == "" && len(v.keys) == 1 {
		for _, single := range v.keys {
			key = single
			ok = true
			break
		}
	}
	v.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: unknown kid", ErrInvalidToken)
	}
	return key, nil
}

func (v *SupabaseVerifier) refreshJWKS(ctx context.Context) error {
	if v.jwksURL == "" {
		return fmt.Errorf("%w: JWKS URL not configured", ErrInvalidToken)
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check after acquiring write lock to avoid stampeding JWKS
	if time.Since(v.lastFetched) < 10*time.Second && len(v.keys) > 0 {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return err
	}

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks endpoint returned HTTP %d", resp.StatusCode)
	}

	var payload jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}

	parsed := make(map[string]any, len(payload.Keys))
	for _, k := range payload.Keys {
		pub, err := parseJWK(k)
		if err != nil {
			continue
		}
		parsed[k.Kid] = pub
	}
	if len(parsed) == 0 {
		return errors.New("jwks contained no usable keys")
	}

	v.keys = parsed
	v.lastFetched = time.Now()
	return nil
}

func parseJWK(k jwkKey) (any, error) {
	switch k.Kty {
	case "EC":
		var curve elliptic.Curve
		switch k.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, fmt.Errorf("unsupported EC curve: %s", k.Crv)
		}
		xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, err
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, err
		}
		return &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}, nil

	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, err
		}
		eInt := new(big.Int).SetBytes(eBytes).Int64()
		return &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(eInt),
		}, nil

	default:
		return nil, fmt.Errorf("unsupported kty: %s", k.Kty)
	}
}
