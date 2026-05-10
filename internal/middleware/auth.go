package middleware

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type AuthType string

const (
	AuthTypeAPIKey AuthType = "apikey"
	AuthTypeJWT    AuthType = "jwt"
	AuthTypeNone   AuthType = "none"
)

type AuthConfig struct {
	Type            AuthType
	APIKeyHeader    string
	APIKeys         map[string]APIKeyEntry
	JWTHMACSecret   string
	JWTAllowedAlgs  []string
	SkipPaths       []string
	Realm           string
}

type APIKeyEntry struct {
	TenantID string
	Scopes   []string
	Active   bool
}

type authMiddleware struct {
	config  AuthConfig
	jwtMu   sync.RWMutex
	jwtNow  func() time.Time
}

func Auth(cfg AuthConfig) gateway.Middleware {
	am := &authMiddleware{
		config: cfg,
		jwtNow: time.Now,
	}
	if am.config.APIKeyHeader == "" {
		am.config.APIKeyHeader = "X-API-Key"
	}
	if am.config.Realm == "" {
		am.config.Realm = "NexusGate"
	}
	if len(am.config.JWTAllowedAlgs) == 0 {
		am.config.JWTAllowedAlgs = []string{"HS256", "HS384", "HS512"}
	}

	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			if am.shouldSkip(req.Path) {
				return next(req)
			}

			switch am.config.Type {
			case AuthTypeAPIKey:
				return am.apiKeyAuth(next, req)
			case AuthTypeJWT:
				return am.jwtAuth(next, req)
			case AuthTypeNone:
				return next(req)
			default:
				return next(req)
			}
		}
	}
}

func (am *authMiddleware) shouldSkip(path string) bool {
	for _, p := range am.config.SkipPaths {
		if p == path {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(path, p[:len(p)-1]) {
			return true
		}
	}
	return false
}

func (am *authMiddleware) apiKeyAuth(next gateway.Handler, req *gateway.Request) (*gateway.Response, error) {
	key := req.Headers.Get(am.config.APIKeyHeader)
	if key == "" {
		return nil, gateway.NewGatewayError(gateway.ErrUnauthorized,
			"missing API key",
			fmt.Sprintf("provide %s header", am.config.APIKeyHeader))
	}

	entry, ok := am.config.APIKeys[key]
	if !ok || !entry.Active {
		slog.Warn("invalid API key attempt",
			"remote", req.RemoteAddr,
			"path", req.Path,
		)
		return nil, gateway.NewGatewayError(gateway.ErrUnauthorized,
			"invalid API key", "key not found or inactive")
	}

	if subtle.ConstantTimeCompare([]byte(key), []byte(key)) != 1 {
		return nil, gateway.NewGatewayError(gateway.ErrUnauthorized,
			"invalid API key", "authentication failed")
	}

	req.TenantID = entry.TenantID

	return next(req)
}

func (am *authMiddleware) jwtAuth(next gateway.Handler, req *gateway.Request) (*gateway.Response, error) {
	authHeader := req.Headers.Get("Authorization")
	if authHeader == "" {
		return nil, gateway.NewGatewayError(gateway.ErrUnauthorized,
			"missing authorization header",
			fmt.Sprintf("provide Bearer token in Authorization header"))
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, gateway.NewGatewayError(gateway.ErrUnauthorized,
			"invalid authorization format", "expected: Bearer <token>")
	}

	tokenStr := parts[1]
	claims, err := am.parseJWT(tokenStr)
	if err != nil {
		slog.Warn("JWT validation failed",
			"remote", req.RemoteAddr,
			"path", req.Path,
			"error", err,
		)
		return nil, gateway.NewGatewayError(gateway.ErrUnauthorized,
			"invalid token", err.Error())
	}

	if sub, ok := claims["sub"].(string); ok {
		req.TenantID = sub
	}

	return next(req)
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

func (am *authMiddleware) parseJWT(tokenStr string) (map[string]interface{}, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format: expected 3 parts")
	}

	headerJSON, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}

	var header jwtHeader
	if err := jsonUnmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("parse header: %w", err)
	}

	algAllowed := false
	for _, alg := range am.config.JWTAllowedAlgs {
		if alg == header.Alg {
			algAllowed = true
			break
		}
	}
	if !algAllowed {
		return nil, fmt.Errorf("algorithm %q not allowed", header.Alg)
	}

	signingInput := parts[0] + "." + parts[1]
	signature, err := base64URLDecode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	if err := am.verifySignature(header.Alg, []byte(signingInput), signature); err != nil {
		return nil, fmt.Errorf("signature verification: %w", err)
	}

	payloadJSON, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var claims map[string]interface{}
	if err := jsonUnmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}

	am.jwtMu.RLock()
	now := am.jwtNow()
	am.jwtMu.RUnlock()

	if exp, ok := claims["exp"].(float64); ok {
		if time.Unix(int64(exp), 0).Before(now) {
			return nil, fmt.Errorf("token expired")
		}
	}

	if nbf, ok := claims["nbf"].(float64); ok {
		if time.Unix(int64(nbf), 0).After(now) {
			return nil, fmt.Errorf("token not yet valid")
		}
	}

	return claims, nil
}

func (am *authMiddleware) verifySignature(alg string, input, signature []byte) error {
	secret := am.config.JWTHMACSecret
	if secret == "" {
		return fmt.Errorf("JWT HMAC secret not configured")
	}

	switch alg {
	case "HS256":
		return verifyHMAC(input, signature, []byte(secret), cryptoSHA256)
	case "HS384":
		return verifyHMAC(input, signature, []byte(secret), cryptoSHA384)
	case "HS512":
		return verifyHMAC(input, signature, []byte(secret), cryptoSHA512)
	default:
		return fmt.Errorf("unsupported algorithm: %s", alg)
	}
}

func base64URLDecode(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.StdEncoding.DecodeString(s)
}
