package proxy

import (
	"io"
	"log/slog"
	"net"
	"strings"

	"github.com/nexusgate/nexusgate/internal/gateway"
	"github.com/nexusgate/nexusgate/internal/router"
)

var wsAllowedHeaders = map[string]bool{
	"upgrade":               true,
	"connection":            true,
	"sec-websocket-key":     true,
	"sec-websocket-version": true,
	"sec-websocket-protocol":true,
	"sec-websocket-extensions":true,
	"origin":                true,
	"cookie":                true,
	"user-agent":            true,
	"x-forwarded-for":       true,
	"x-forwarded-proto":     true,
	"x-real-ip":             true,
	"x-request-id":          true,
	"x-tenant-id":           true,
	"authorization":         true,
}

func sanitizeHeaderValue(v string) string {
	s := strings.ReplaceAll(v, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

func IsWebSocketUpgrade(req *gateway.Request) bool {
	return strings.EqualFold(req.Headers.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(req.Headers.Get("Connection")), "upgrade")
}

func (p *Proxy) ForwardWebSocket(req *gateway.Request, backend *router.Backend, route *router.Route) error {
	targetAddr := backend.Address
	if strings.HasPrefix(targetAddr, "http://") {
		targetAddr = strings.TrimPrefix(targetAddr, "http://")
	} else if strings.HasPrefix(targetAddr, "https://") {
		targetAddr = strings.TrimPrefix(targetAddr, "https://")
	}

	backendConn, err := net.DialTimeout("tcp", targetAddr, p.webSocketConnectTimeout)
	if err != nil {
		p.tracker.MarkUnhealthy(backend.Address)
		if route != nil {
			if nextBackend := p.nextBackend(route, map[string]bool{backend.Address: true}); nextBackend != nil {
				slog.Info("websocket retrying with different backend", "backend", nextBackend.Address)
				return p.ForwardWebSocket(req, nextBackend, route)
			}
		}
		return gateway.NewGatewayErrorWithCause(gateway.ErrBackendDown, "websocket backend unreachable", err)
	}
	defer backendConn.Close()

	handshake := buildWebSocketHandshake(req, sanitizeHeaderValue(targetAddr))
	if _, err := backendConn.Write(handshake); err != nil {
		return gateway.NewGatewayErrorWithCause(gateway.ErrBackendDown, "websocket handshake write failed", err)
	}

	buf := make([]byte, 4096)
	n, err := backendConn.Read(buf)
	if err != nil {
		return gateway.NewGatewayErrorWithCause(gateway.ErrBackendDown, "websocket handshake read failed", err)
	}

	clientConn := req.RawConn
	if clientConn == nil {
		return gateway.NewGatewayError(gateway.ErrInternal, "no raw connection",
			"websocket requires raw TCP connection access")
	}

	if _, err := clientConn.Write(buf[:n]); err != nil {
		return gateway.NewGatewayErrorWithCause(gateway.ErrBackendDown, "websocket response write failed", err)
	}

	slog.Info("websocket tunnel established",
		"path", req.Path,
		"backend", targetAddr,
		"client", req.RemoteAddr,
	)

	errCh := make(chan error, 2)

	go func() {
		_, err := io.Copy(backendConn, clientConn)
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(clientConn, backendConn)
		errCh <- err
	}()

	<-errCh

	slog.Info("websocket tunnel closed",
		"path", req.Path,
		"backend", targetAddr,
	)

	return nil
}

func buildWebSocketHandshake(req *gateway.Request, host string) []byte {
	var b strings.Builder
	b.WriteString(req.Method)
	b.WriteString(" ")
	b.WriteString(req.Path)
	if req.QueryString != "" {
		b.WriteString("?")
		b.WriteString(req.QueryString)
	}
	b.WriteString(" HTTP/1.1\r\n")
	b.WriteString("Host: ")
	b.WriteString(host)
	b.WriteString("\r\n")

	for key, values := range req.Headers {
		lower := strings.ToLower(key)
		if !wsAllowedHeaders[lower] {
			continue
		}
		for _, value := range values {
			b.WriteString(key)
			b.WriteString(": ")
			b.WriteString(sanitizeHeaderValue(value))
			b.WriteString("\r\n")
		}
	}

	b.WriteString("\r\n")
	return []byte(b.String())
}
