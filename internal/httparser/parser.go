package httparser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

const (
	maxHeaderLines = 256
	maxLineLength  = 8192
)

type Parser struct {
	maxHeaderBytes int64
	maxBodyBytes   int64
	readTimeout    time.Duration
}

func NewParser() *Parser {
	return &Parser{
		maxHeaderBytes: 1 << 20,
		maxBodyBytes:   10 << 20,
		readTimeout:    30 * time.Second,
	}
}

func (p *Parser) WithMaxHeaderBytes(n int64) *Parser {
	p.maxHeaderBytes = n
	return p
}

func (p *Parser) WithMaxBodyBytes(n int64) *Parser {
	p.maxBodyBytes = n
	return p
}

func (p *Parser) WithReadTimeout(d time.Duration) *Parser {
	p.readTimeout = d
	return p
}

func (p *Parser) ParseRequest(conn net.Conn) (*gateway.Request, error) {
	if p.readTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(p.readTimeout))
	}

	reader := bufio.NewReaderSize(conn, 4096)

	requestLine, err := readLine(reader)
	if err != nil {
		return nil, fmt.Errorf("read request line: %w", err)
	}

	method, path, proto, err := parseRequestLine(requestLine)
	if err != nil {
		return nil, err
	}

	if proto != "HTTP/1.1" && proto != "HTTP/1.0" {
		return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
			"unsupported protocol", proto)
	}

	headers, err := readHeaders(reader, p.maxHeaderBytes)
	if err != nil {
		return nil, err
	}

	body, err := readBody(reader, headers, method, p.maxBodyBytes)
	if err != nil {
		return nil, err
	}

	pathAndQuery := path
	queryString := ""
	if idx := strings.Index(path, "?"); idx >= 0 {
		pathAndQuery = path[:idx]
		queryString = path[idx+1:]
	}

	host := headers.Get("Host")
	if host == "" {
		host = conn.LocalAddr().String()
	}

	tenantID := headers.Get("X-Tenant-ID")
	if tenantID == "" {
		tenantID = "default"
	}

	scheme := "http"
	if headers.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}

	req := &gateway.Request{
		Method:      method,
		Path:        pathAndQuery,
		Host:        host,
		Headers:     headers,
		QueryString: queryString,
		Body:        body,
		RawConn:     conn,
		RemoteAddr:  conn.RemoteAddr().String(),
		Scheme:      scheme,
		TenantID:    tenantID,
	}

	return req, nil
}

func (p *Parser) ParseResponse(reader *bufio.Reader) (*gateway.Response, error) {
	statusLine, err := readLine(reader)
	if err != nil {
		return nil, fmt.Errorf("read status line: %w", err)
	}

	proto, statusCode, _, err := parseStatusLine(statusLine)
	if err != nil {
		return nil, err
	}

	if proto != "HTTP/1.1" && proto != "HTTP/1.0" {
		return nil, fmt.Errorf("unsupported response protocol: %s", proto)
	}

	headers, err := readHeaders(reader, p.maxHeaderBytes)
	if err != nil {
		return nil, fmt.Errorf("read response headers: %w", err)
	}

	body, err := readResponseBody(reader, headers, statusCode, p.maxBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return &gateway.Response{
		StatusCode: statusCode,
		Headers:    headers,
		Body:       body,
	}, nil
}

func parseStatusLine(line string) (proto string, statusCode int, statusText string, err error) {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return "", 0, "", gateway.NewGatewayError(gateway.ErrBadRequest,
			"malformed status line", line)
	}

	proto = parts[0]
	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", gateway.NewGatewayError(gateway.ErrBadRequest,
			"invalid status code", parts[1])
	}

	statusText = ""
	if len(parts) > 2 {
		statusText = parts[2]
	}

	return proto, code, statusText, nil
}

func readHeaders(reader *bufio.Reader, maxHeaderBytes int64) (http.Header, error) {
	headers := http.Header{}
	var totalHeaderSize int64
	var lineCount int

	for {
		if lineCount >= maxHeaderLines {
			return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
				"too many header lines", fmt.Sprintf("exceeded %d", maxHeaderLines))
		}

		line, err := readLine(reader)
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}

		if line == "" {
			break
		}

		lineCount++
		totalHeaderSize += int64(len(line))
		if totalHeaderSize > maxHeaderBytes {
			return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
				"headers too large", fmt.Sprintf("exceeded %d bytes", maxHeaderBytes))
		}

		key, value, err := parseHeaderLine(line)
		if err != nil {
			continue
		}
		headers.Add(key, value)
	}

	return headers, nil
}

func readBody(reader *bufio.Reader, headers http.Header, method string, maxBodyBytes int64) ([]byte, error) {
	if method == http.MethodGet || method == http.MethodHead {
		return nil, nil
	}

	transferEncoding := headers.Get("Transfer-Encoding")
	if isChunked(transferEncoding) {
		return readChunkedBody(reader, maxBodyBytes)
	}

	contentLengthStr := headers.Get("Content-Length")
	if contentLengthStr != "" {
		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64)
		if err != nil {
			return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
				"invalid content-length", contentLengthStr)
		}
		if contentLength < 0 {
			return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
				"negative content-length", contentLengthStr)
		}
		if contentLength > maxBodyBytes {
			return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
				"body too large", fmt.Sprintf("exceeded %d bytes", maxBodyBytes))
		}
		if contentLength > 0 {
			body := make([]byte, contentLength)
			if _, err := io.ReadFull(reader, body); err != nil {
				return nil, fmt.Errorf("read body: %w", err)
			}
			return body, nil
		}
	}

	return nil, nil
}

func readResponseBody(reader *bufio.Reader, headers http.Header, statusCode int, maxBodyBytes int64) ([]byte, error) {
	if statusCode == http.StatusNoContent || statusCode == http.StatusNotModified {
		return nil, nil
	}

	transferEncoding := headers.Get("Transfer-Encoding")
	if isChunked(transferEncoding) {
		return readChunkedBody(reader, maxBodyBytes)
	}

	contentLengthStr := headers.Get("Content-Length")
	if contentLengthStr != "" {
		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64)
		if err != nil {
			return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
				"invalid content-length", contentLengthStr)
		}
		if contentLength < 0 {
			return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
				"negative content-length", contentLengthStr)
		}
		if contentLength > maxBodyBytes {
			return nil, gateway.NewGatewayError(gateway.ErrBadRequest,
				"body too large", fmt.Sprintf("exceeded %d bytes", maxBodyBytes))
		}
		if contentLength > 0 {
			body := make([]byte, contentLength)
			if _, err := io.ReadFull(reader, body); err != nil {
				return nil, fmt.Errorf("read body: %w", err)
			}
			return body, nil
		}
		return nil, nil
	}

	if headers.Get("Content-Length") == "" && !isChunked(transferEncoding) {
		body, err := io.ReadAll(io.LimitReader(reader, maxBodyBytes))
		if err != nil {
			return nil, fmt.Errorf("read body until close: %w", err)
		}
		return body, nil
	}

	return nil, nil
}

func isChunked(transferEncoding string) bool {
	for _, te := range strings.Split(transferEncoding, ",") {
		if strings.EqualFold(strings.TrimSpace(te), "chunked") {
			return true
		}
	}
	return false
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return "", io.ErrUnexpectedEOF
		}
		return "", err
	}
	if len(line) > maxLineLength {
		return "", gateway.NewGatewayError(gateway.ErrBadRequest,
			"header line too long", fmt.Sprintf("exceeded %d bytes", maxLineLength))
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func parseRequestLine(line string) (method, path, proto string, err error) {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) != 3 {
		return "", "", "", gateway.NewGatewayError(gateway.ErrBadRequest,
			"malformed request line", line)
	}
	return parts[0], parts[1], parts[2], nil
}

func parseHeaderLine(line string) (key, value string, err error) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("malformed header: %s", line)
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	return key, value, nil
}

func readChunkedBody(reader *bufio.Reader, maxBytes int64) ([]byte, error) {
	var body []byte
	var total int64
	crlf := make([]byte, 2)
	for {
		line, err := readLine(reader)
		if err != nil {
			return nil, fmt.Errorf("read chunk size: %w", err)
		}

		chunkSizeStr := line
		if idx := strings.Index(chunkSizeStr, ";"); idx >= 0 {
			chunkSizeStr = chunkSizeStr[:idx]
		}
		chunkSizeStr = strings.TrimSpace(chunkSizeStr)

		chunkSize, err := strconv.ParseInt(chunkSizeStr, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid chunk size: %q", chunkSizeStr)
		}
		if chunkSize == 0 {
			readTrailerHeaders(reader)
			break
		}
		if chunkSize < 0 {
			return nil, fmt.Errorf("negative chunk size: %d", chunkSize)
		}

		total += chunkSize
		if total > maxBytes {
			return nil, fmt.Errorf("chunked body exceeded %d bytes", maxBytes)
		}

		if cap(body)-len(body) < int(chunkSize) {
			newCap := cap(body) * 2
			if newCap < int(total) {
				newCap = int(total)
			}
			newBody := make([]byte, len(body), newCap)
			copy(newBody, body)
			body = newBody
		}

		chunkStart := len(body)
		body = body[:chunkStart+int(chunkSize)]
		if _, err := io.ReadFull(reader, body[chunkStart:]); err != nil {
			return nil, fmt.Errorf("read chunk data: %w", err)
		}

		if _, err := io.ReadFull(reader, crlf); err != nil {
			return nil, fmt.Errorf("read chunk CRLF: %w", err)
		}
		if crlf[0] != '\r' || crlf[1] != '\n' {
			return nil, fmt.Errorf("missing CRLF after chunk data")
		}
	}
	return body, nil
}

func readTrailerHeaders(reader *bufio.Reader) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return
		}
	}
}

func WriteResponse(conn net.Conn, resp *gateway.Response) error {
	if resp.Headers == nil {
		resp.Headers = http.Header{}
	}

	if resp.StatusCode == 0 {
		resp.StatusCode = 200
	}

	statusText := http.StatusText(resp.StatusCode)
	if _, err := fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\n", resp.StatusCode, statusText); err != nil {
		return err
	}

	if resp.Body != nil {
		resp.Headers.Set("Content-Length", strconv.Itoa(len(resp.Body)))
	}

	for key, values := range resp.Headers {
		for _, value := range values {
			safeKey := sanitizeHeader(key)
			safeValue := sanitizeHeader(value)
			if _, err := fmt.Fprintf(conn, "%s: %s\r\n", safeKey, safeValue); err != nil {
				return err
			}
		}
	}

	if _, err := conn.Write([]byte("\r\n")); err != nil {
		return err
	}

	if resp.Body != nil && len(resp.Body) > 0 {
		if _, err := conn.Write(resp.Body); err != nil {
			return err
		}
	}

	return nil
}

func sanitizeHeader(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

func WriteErrorResponse(conn net.Conn, gwErr *gateway.GatewayError) {
	errBody := struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Detail  string `json:"detail,omitempty"`
	}{
		Code:    int(gwErr.Code),
		Message: gwErr.Message,
		Detail:  gwErr.Detail,
	}
	body, _ := json.Marshal(errBody)
	resp := &gateway.Response{
		StatusCode: gwErr.HTTPStatus(),
		Headers: http.Header{
			"Content-Type": {"application/json"},
		},
		Body: body,
	}
	if err := WriteResponse(conn, resp); err != nil {
		slog.Error("failed to write error response", "error", err)
	}
}
