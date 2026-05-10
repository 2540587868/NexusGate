package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestAuthSkipPaths(t *testing.T) {
	cfg := AuthConfig{
		Type:      AuthTypeAPIKey,
		SkipPaths: []string{"/healthz", "/public/*"},
		APIKeys: map[string]APIKeyEntry{
			"test-key": {TenantID: "tenant1", Active: true},
		},
	}

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	tests := []struct {
		path       string
		expectPass bool
	}{
		{"/healthz", true},
		{"/public/assets/main.js", true},
		{"/api/users", false},
		{"/private/data", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := &gateway.Request{
				Path:    tt.path,
				Headers: http.Header{},
			}
			resp, err := handler(req)
			if tt.expectPass {
				if err != nil {
					t.Errorf("path %q should be skipped, got error: %v", tt.path, err)
				}
				if resp.StatusCode != 200 {
					t.Errorf("path %q should return 200, got %d", tt.path, resp.StatusCode)
				}
			} else {
				if err == nil {
					t.Errorf("path %q should require auth", tt.path)
				}
			}
		})
	}
}

func TestAuthAPIKeyValid(t *testing.T) {
	cfg := AuthConfig{
		Type:         AuthTypeAPIKey,
		APIKeyHeader: "X-API-Key",
		APIKeys: map[string]APIKeyEntry{
			"valid-key": {TenantID: "tenant1", Scopes: []string{"read"}, Active: true},
		},
	}

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		if req.TenantID != "tenant1" {
			t.Errorf("expected TenantID tenant1, got %q", req.TenantID)
		}
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{},
	}
	req.Headers.Set("X-API-Key", "valid-key")
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("valid API key should pass: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthAPIKeyMissing(t *testing.T) {
	cfg := AuthConfig{
		Type:   AuthTypeAPIKey,
		APIKeys: map[string]APIKeyEntry{},
	}

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{},
	}
	_, err := handler(req)
	if err == nil {
		t.Error("missing API key should fail")
	}
	gwErr, ok := err.(*gateway.GatewayError)
	if !ok || gwErr.Code != gateway.ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestAuthAPIKeyInvalid(t *testing.T) {
	cfg := AuthConfig{
		Type:   AuthTypeAPIKey,
		APIKeys: map[string]APIKeyEntry{
			"valid-key": {TenantID: "tenant1", Active: true},
		},
	}

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"X-API-Key": {"invalid-key"}},
	}
	_, err := handler(req)
	if err == nil {
		t.Error("invalid API key should fail")
	}
}

func TestAuthAPIKeyInactive(t *testing.T) {
	cfg := AuthConfig{
		Type:   AuthTypeAPIKey,
		APIKeys: map[string]APIKeyEntry{
			"inactive-key": {TenantID: "tenant1", Active: false},
		},
	}

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"X-API-Key": {"inactive-key"}},
	}
	_, err := handler(req)
	if err == nil {
		t.Error("inactive API key should fail")
	}
}

func TestAuthJWTValid(t *testing.T) {
	secret := "test-secret-key"
	cfg := AuthConfig{
		Type:          AuthTypeJWT,
		JWTHMACSecret: secret,
		JWTAllowedAlgs: []string{"HS256"},
	}

	token := createTestJWT(t, secret, map[string]interface{}{
		"sub": "tenant1",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		if req.TenantID != "tenant1" {
			t.Errorf("expected TenantID tenant1, got %q", req.TenantID)
		}
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Authorization": {"Bearer " + token}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("valid JWT should pass: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthJWTExpired(t *testing.T) {
	secret := "test-secret-key"
	cfg := AuthConfig{
		Type:          AuthTypeJWT,
		JWTHMACSecret: secret,
		JWTAllowedAlgs: []string{"HS256"},
	}

	token := createTestJWT(t, secret, map[string]interface{}{
		"sub": "tenant1",
		"exp": float64(time.Now().Add(-1 * time.Hour).Unix()),
	})

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Authorization": {"Bearer " + token}},
	}
	_, err := handler(req)
	if err == nil {
		t.Error("expired JWT should fail")
	}
}

func TestAuthJWTMissingBearer(t *testing.T) {
	cfg := AuthConfig{
		Type:          AuthTypeJWT,
		JWTHMACSecret: "secret",
	}

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	tests := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"no bearer", "Basic dXNlcjpwYXNz"},
		{"bad format", "Bearer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &gateway.Request{
				Headers: http.Header{"Authorization": {tt.header}},
			}
			_, err := handler(req)
			if err == nil {
				t.Error("should fail with missing or malformed Authorization header")
			}
		})
	}
}

func TestAuthJWTBadSignature(t *testing.T) {
	cfg := AuthConfig{
		Type:          AuthTypeJWT,
		JWTHMACSecret: "correct-secret",
		JWTAllowedAlgs: []string{"HS256"},
	}

	token := createTestJWT(t, "wrong-secret", map[string]interface{}{
		"sub": "tenant1",
		"exp": float64(time.Now().Add(1 * time.Hour).Unix()),
	})

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Authorization": {"Bearer " + token}},
	}
	_, err := handler(req)
	if err == nil {
		t.Error("JWT with bad signature should fail")
	}
}

func TestAuthNone(t *testing.T) {
	cfg := AuthConfig{
		Type: AuthTypeNone,
	}

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("none auth should pass: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAuthDefaultTypeIsNone(t *testing.T) {
	cfg := AuthConfig{}

	mw := Auth(cfg)
	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{StatusCode: 200}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("empty config should default to none auth: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func createTestJWT(t *testing.T, secret string, claims map[string]interface{}) string {
	t.Helper()

	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(claims)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	signingInput := headerB64 + "." + payloadB64
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	signature := mac.Sum(nil)
	sigB64 := base64.RawURLEncoding.EncodeToString(signature)

	return fmt.Sprintf("%s.%s.%s", headerB64, payloadB64, sigB64)
}
