package httparser

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestParseRequestGET(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		client.Write([]byte("GET /api/v1/users?limit=10 HTTP/1.1\r\nHost: example.com\r\nX-Tenant-ID: tenant-A\r\nX-Forwarded-Proto: https\r\n\r\n"))
	}()

	parser := NewParser()
	req, err := parser.ParseRequest(server)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if req.Method != "GET" {
		t.Errorf("Method = %q, want GET", req.Method)
	}
	if req.Path != "/api/v1/users" {
		t.Errorf("Path = %q, want /api/v1/users", req.Path)
	}
	if req.QueryString != "limit=10" {
		t.Errorf("QueryString = %q, want limit=10", req.QueryString)
	}
	if req.Host != "example.com" {
		t.Errorf("Host = %q, want example.com", req.Host)
	}
	if req.TenantID != "tenant-A" {
		t.Errorf("TenantID = %q, want tenant-A", req.TenantID)
	}
	if req.Scheme != "https" {
		t.Errorf("Scheme = %q, want https", req.Scheme)
	}
}

func TestParseRequestPOST(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	body := `{"name":"test"}`
	go func() {
		request := fmt.Sprintf("POST /api/v1/users HTTP/1.1\r\nHost: example.com\r\nContent-Length: %d\r\nContent-Type: application/json\r\n\r\n%s", len(body), body)
		client.Write([]byte(request))
	}()

	parser := NewParser()
	req, err := parser.ParseRequest(server)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if req.Method != "POST" {
		t.Errorf("Method = %q, want POST", req.Method)
	}
	if string(req.Body) != body {
		t.Errorf("Body = %q, want %q", string(req.Body), body)
	}
}

func TestParseRequestDefaultTenant(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		client.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
	}()

	parser := NewParser()
	req, err := parser.ParseRequest(server)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if req.TenantID != "default" {
		t.Errorf("TenantID = %q, want default", req.TenantID)
	}
}

func TestParseRequestUnsupportedProtocol(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		client.Write([]byte("GET / HTTP/2.0\r\nHost: localhost\r\n\r\n"))
	}()

	parser := NewParser()
	_, err := parser.ParseRequest(server)
	if err == nil {
		t.Error("expected error for unsupported protocol, got nil")
	}
}

func TestWriteResponse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	resp := &gateway.Response{
		StatusCode: 200,
		Headers:    http.Header{"Content-Type": {"text/plain"}},
		Body:       []byte("hello"),
	}

	go func() {
		WriteResponse(server, resp)
		server.Close()
	}()

	reader := bufio.NewReader(client)
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "200") {
		t.Errorf("status line = %q, should contain 200", line)
	}
}

func TestWriteErrorResponse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	gwErr := gateway.NewGatewayError(gateway.ErrRateLimited, "rate limited", "too many requests")

	go func() {
		WriteErrorResponse(server, gwErr)
		server.Close()
	}()

	reader := bufio.NewReader(client)
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "429") {
		t.Errorf("status line = %q, should contain 429", line)
	}
}

func TestParseRequestMalformed(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		client.Write([]byte("INVALID\r\n\r\n"))
	}()

	parser := NewParser()
	_, err := parser.ParseRequest(server)
	if err == nil {
		t.Error("expected error for malformed request")
	}
}

func TestParseRequestConnectionClose(t *testing.T) {
	server, client := net.Pipe()

	go func() {
		client.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"))
		client.Close()
	}()

	parser := NewParser()
	_, err := parser.ParseRequest(server)
	server.Close()
	if err != nil {
		t.Logf("ParseRequest with closed connection returned: %v (acceptable)", err)
	}
}

func TestParseRequestChunkedBody(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		request := "POST /upload HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
		client.Write([]byte(request))
	}()

	parser := NewParser()
	req, err := parser.ParseRequest(server)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if string(req.Body) != "hello world" {
		t.Errorf("Body = %q, want 'hello world'", string(req.Body))
	}
}

func TestParseRequestContentLengthZero(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		client.Write([]byte("POST /api HTTP/1.1\r\nHost: example.com\r\nContent-Length: 0\r\n\r\n"))
	}()

	parser := NewParser()
	req, err := parser.ParseRequest(server)
	if err != nil {
		t.Fatalf("ParseRequest() error = %v", err)
	}

	if req.Body != nil && len(req.Body) != 0 {
		t.Errorf("Body should be empty, got %q", string(req.Body))
	}
}

func TestWriteResponseWithHeaders(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	resp := &gateway.Response{
		StatusCode: 201,
		Headers: http.Header{
			"Content-Type": {"application/json"},
			"X-Custom":     {"test-value"},
		},
		Body: []byte(`{"status":"created"}`),
	}

	go func() {
		WriteResponse(server, resp)
		server.Close()
	}()

	reader := bufio.NewReader(client)
	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "201") {
		t.Errorf("status line = %q, should contain 201", line)
	}
}

func TestGatewayErrorHTTPStatus(t *testing.T) {
	tests := []struct {
		code     gateway.ErrorCode
		expected int
	}{
		{gateway.ErrBadRequest, 400},
		{gateway.ErrUnauthorized, 401},
		{gateway.ErrRateLimited, 429},
		{gateway.ErrNoRoute, 404},
		{gateway.ErrBackendDown, 502},
		{gateway.ErrBackendTimeout, 504},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("code_%d", tt.code), func(t *testing.T) {
			gwErr := gateway.NewGatewayError(tt.code, "test", "test detail")
			if gwErr.HTTPStatus() != tt.expected {
				t.Errorf("HTTPStatus() = %d, want %d", gwErr.HTTPStatus(), tt.expected)
			}
		})
	}
}
