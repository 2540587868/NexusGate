package middleware

import (
	"compress/gzip"
	"io"
	"strings"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type CompressionConfig struct {
	Level      int  `yaml:"level"`
	MinSize    int  `yaml:"min_size"`
	EnableGzip bool `yaml:"enable_gzip"`
}

func Compression(cfg CompressionConfig) gateway.Middleware {
	if cfg.Level == 0 {
		cfg.Level = gzip.DefaultCompression
	}
	if cfg.MinSize == 0 {
		cfg.MinSize = 1024
	}
	if !cfg.EnableGzip {
		cfg.EnableGzip = true
	}

	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			resp, err := next(req)
			if err != nil {
				return resp, err
			}

			if resp == nil || resp.Body == nil || len(resp.Body) < cfg.MinSize {
				return resp, nil
			}

			if resp.StreamBody != nil {
				return resp, nil
			}

			if !cfg.EnableGzip {
				return resp, nil
			}

			acceptEncoding := req.Headers.Get("Accept-Encoding")
			if !strings.Contains(acceptEncoding, "gzip") {
				return resp, nil
			}

			contentType := resp.Headers.Get("Content-Type")
			if isCompressibleContentType(contentType) {
				compressed, err := gzipCompress(resp.Body, cfg.Level)
				if err != nil {
					return resp, nil
				}

				if len(compressed) >= len(resp.Body) {
					return resp, nil
				}

				resp.Body = compressed
				resp.Headers.Set("Content-Encoding", "gzip")
				resp.Headers.Del("Content-Length")
				resp.Headers.Add("Vary", "Accept-Encoding")
			}

			return resp, nil
		}
	}
}

func gzipCompress(data []byte, level int) ([]byte, error) {
	w := &gzipWriter{level: level}
	gw, err := gzip.NewWriterLevel(w, level)
	if err != nil {
		return nil, err
	}

	if _, err := gw.Write(data); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return w.Bytes(), nil
}

type gzipWriter struct {
	level int
	buf   []byte
}

func (w *gzipWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *gzipWriter) Bytes() []byte {
	return w.buf
}

func isCompressibleContentType(contentType string) bool {
	if contentType == "" {
		return true
	}

	compressible := []string{
		"text/",
		"application/json",
		"application/javascript",
		"application/xml",
		"application/svg",
		"application/x-yaml",
	}

	for _, ct := range compressible {
		if strings.Contains(contentType, ct) {
			return true
		}
	}

	notCompressible := []string{
		"image/",
		"video/",
		"audio/",
		"application/zip",
		"application/gzip",
		"application/x-gzip",
		"application/octet-stream",
	}

	for _, ct := range notCompressible {
		if strings.Contains(contentType, ct) {
			return false
		}
	}

	return false
}

func DecompressGzipBody(body io.Reader) ([]byte, error) {
	gr, err := gzip.NewReader(body)
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	return io.ReadAll(gr)
}
