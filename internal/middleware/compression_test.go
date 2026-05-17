package middleware

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

func TestCompressionGzipResponse(t *testing.T) {
	cfg := CompressionConfig{
		EnableGzip: true,
		MinSize:    100,
	}

	mw := Compression(cfg)
	body := []byte(strings.Repeat("hello world ", 50))

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"text/plain"}},
			Body:       body,
		}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Accept-Encoding": {"gzip"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("Content-Encoding") != "gzip" {
		t.Errorf("expected Content-Encoding=gzip, got %q", resp.Headers.Get("Content-Encoding"))
	}
	if bytes.Equal(resp.Body, body) {
		t.Error("response body should be compressed")
	}
}

func TestCompressionNoGzipAccept(t *testing.T) {
	cfg := CompressionConfig{
		EnableGzip: true,
		MinSize:    100,
	}

	mw := Compression(cfg)
	body := []byte(strings.Repeat("hello world ", 50))

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"text/plain"}},
			Body:       body,
		}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("Content-Encoding") == "gzip" {
		t.Error("should not compress without Accept-Encoding: gzip")
	}
	if !bytes.Equal(resp.Body, body) {
		t.Error("body should be unchanged")
	}
}

func TestCompressionSmallBody(t *testing.T) {
	cfg := CompressionConfig{
		EnableGzip: true,
		MinSize:    1024,
	}

	mw := Compression(cfg)
	body := []byte("small")

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"text/plain"}},
			Body:       body,
		}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Accept-Encoding": {"gzip"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("Content-Encoding") == "gzip" {
		t.Error("small body should not be compressed")
	}
}

func TestCompressionNotCompressibleType(t *testing.T) {
	cfg := CompressionConfig{
		EnableGzip: true,
		MinSize:    100,
	}

	mw := Compression(cfg)
	body := []byte(strings.Repeat("x", 2000))

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"image/png"}},
			Body:       body,
		}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Accept-Encoding": {"gzip"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("Content-Encoding") == "gzip" {
		t.Error("image/png should not be compressed")
	}
}

func TestCompressionRatioCheck(t *testing.T) {
	cfg := CompressionConfig{
		EnableGzip: true,
		MinSize:    10,
	}

	mw := Compression(cfg)
	body := make([]byte, 2000)
	rand.Read(body)

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"text/plain"}},
			Body:       body,
		}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Accept-Encoding": {"gzip"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("Content-Encoding") == "gzip" {
		t.Error("compressed size >= original should not apply compression")
	}
}

func TestCompressionStreamBody(t *testing.T) {
	cfg := CompressionConfig{
		EnableGzip: true,
		MinSize:    100,
	}

	mw := Compression(cfg)
	body := []byte(strings.Repeat("hello world ", 50))

	handler := mw(func(req *gateway.Request) (*gateway.Response, error) {
		return &gateway.Response{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": {"text/plain"}},
			Body:       body,
			StreamBody: io.NopCloser(strings.NewReader("stream")),
		}, nil
	})

	req := &gateway.Request{
		Headers: http.Header{"Accept-Encoding": {"gzip"}},
	}
	resp, err := handler(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers.Get("Content-Encoding") == "gzip" {
		t.Error("stream body should not be compressed")
	}
}

func TestIsCompressibleContentType(t *testing.T) {
	tests := []struct {
		contentType string
		want        bool
	}{
		{"text/plain", true},
		{"text/html; charset=utf-8", true},
		{"application/json", true},
		{"application/javascript", true},
		{"application/xml", true},
		{"application/svg", true},
		{"application/x-yaml", true},
		{"image/png", false},
		{"image/jpeg", false},
		{"video/mp4", false},
		{"audio/mpeg", false},
		{"application/zip", false},
		{"application/gzip", false},
		{"application/x-gzip", false},
		{"application/octet-stream", false},
		{"", true},
		{"application/unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			got := isCompressibleContentType(tt.contentType)
			if got != tt.want {
				t.Errorf("isCompressibleContentType(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestDecompressGzipBody(t *testing.T) {
	original := []byte(strings.Repeat("test data for compression round trip ", 20))

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write(original)
	gw.Close()

	decompressed, err := DecompressGzipBody(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(decompressed, original) {
		t.Errorf("decompressed data does not match original")
	}
}
