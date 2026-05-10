package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type CORSOptions struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

func CORS(opts CORSOptions) gateway.Middleware {
	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			if req.Method == "OPTIONS" {
				resp := &gateway.Response{
					StatusCode: http.StatusNoContent,
					Headers:    http.Header{},
				}
				applyCORSHeaders(resp, req, opts)
				return resp, nil
			}

			resp, err := next(req)
			if resp != nil {
				applyCORSHeaders(resp, req, opts)
			}
			return resp, err
		}
	}
}

func applyCORSHeaders(resp *gateway.Response, req *gateway.Request, opts CORSOptions) {
	origin := req.Headers.Get("Origin")
	if origin == "" {
		return
	}

	if opts.AllowCredentials && hasWildcardOrigin(opts.AllowOrigins) {
		slog.Warn("CORS misconfiguration: AllowCredentials=true with wildcard origin is insecure, credentials will not be sent")
	}

	allowed := isOriginAllowed(origin, opts.AllowOrigins)
	if !allowed {
		return
	}

	if hasWildcardOrigin(opts.AllowOrigins) && !opts.AllowCredentials {
		resp.Headers.Set("Access-Control-Allow-Origin", "*")
	} else {
		resp.Headers.Set("Access-Control-Allow-Origin", origin)
		resp.Headers.Set("Vary", "Origin")
	}

	if len(opts.AllowMethods) > 0 {
		resp.Headers.Set("Access-Control-Allow-Methods", strings.Join(opts.AllowMethods, ", "))
	}

	if len(opts.AllowHeaders) > 0 {
		resp.Headers.Set("Access-Control-Allow-Headers", strings.Join(opts.AllowHeaders, ", "))
	}

	if len(opts.ExposeHeaders) > 0 {
		resp.Headers.Set("Access-Control-Expose-Headers", strings.Join(opts.ExposeHeaders, ", "))
	}

	if opts.AllowCredentials {
		resp.Headers.Set("Access-Control-Allow-Credentials", "true")
	}

	if opts.MaxAge > 0 {
		resp.Headers.Set("Access-Control-Max-Age", strconv.Itoa(opts.MaxAge))
	}
}

func isOriginAllowed(origin string, allowOrigins []string) bool {
	if len(allowOrigins) == 0 {
		return false
	}
	for _, o := range allowOrigins {
		if o == "*" || o == origin {
			return true
		}
	}
	return false
}

func hasWildcardOrigin(allowOrigins []string) bool {
	for _, o := range allowOrigins {
		if o == "*" {
			return true
		}
	}
	return false
}
