package middleware

import (
	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/util"
)

const RequestIDHeader = "X-Request-ID"

func RequestID(next gateway.Handler) gateway.Handler {
	return func(req *gateway.Request) (*gateway.Response, error) {
		if id := req.Headers.Get(RequestIDHeader); id != "" {
			if req.ID == "" {
				req.ID = id
			}
		} else {
			id = util.GenerateRandomID(16)
			req.Headers.Set(RequestIDHeader, id)
			if req.ID == "" {
				req.ID = id
			}
		}

		resp, err := next(req)

		if resp != nil && resp.Headers != nil {
			if resp.Headers.Get(RequestIDHeader) == "" {
				resp.Headers.Set(RequestIDHeader, req.ID)
			}
		}

		return resp, err
	}
}
