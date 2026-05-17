package middleware

import (
	"github.com/nexusgate/nexusgate/internal/gateway"
)

type GRPCTranscoderConfig struct {
	Enable    bool              `yaml:"enable"`
	Services  map[string]string `yaml:"services"`
}

func GRPCTranscoder(cfg GRPCTranscoderConfig) gateway.Middleware {
	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			contentType := req.Headers.Get("Content-Type")
			if contentType == "application/grpc" || contentType == "application/grpc+proto" {
				req.Headers.Set("Content-Type", "application/json")
				req.Headers.Set("X-Original-Content-Type", contentType)
				req.Headers.Set("X-GRPC-Transcoded", "true")
			}

			resp, err := next(req)
			if err != nil {
				return resp, err
			}

			if resp != nil && req.Headers.Get("X-GRPC-Transcoded") == "true" {
				resp.Headers.Del("Grpc-Status")
				resp.Headers.Del("Grpc-Message")
				resp.Headers.Set("Content-Type", "application/json")
			}

			return resp, nil
		}
	}
}
