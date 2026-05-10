package gateway

import (
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"time"
)

const ShardCount = 8

type Request struct {
	ID          string
	TenantID    string
	Method      string
	Path        string
	Host        string
	Headers     http.Header
	QueryString string
	Body        []byte
	RawConn     net.Conn
	RemoteAddr  string
	Scheme      string

	shardKey  uint32
	startTime time.Time
	routeKey  string
	RespCh    chan *ResponseResult
}

type ResponseResult struct {
	Resp *Response
	Err  error
}

func (r *Request) RouteKey() string {
	if r.routeKey == "" {
		r.routeKey = r.Method + " " + r.Path
	}
	return r.routeKey
}

func (r *Request) ShardKey() uint32 {
	if r.shardKey == 0 {
		h := fnv.New32a()
		h.Write([]byte(r.TenantID))
		r.shardKey = h.Sum32() % ShardCount
	}
	return r.shardKey
}

func (r *Request) StartTime() time.Time {
	return r.startTime
}

type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

type Handler func(*Request) (*Response, error)

type Middleware func(next Handler) Handler

type ProxyMode int

const (
	ProxyModeSplice ProxyMode = iota
	ProxyModeMmap
	ProxyModeBuffer
)

func DetermineProxyMode(req *Request) ProxyMode {
	if req.Method == http.MethodGet || req.Method == http.MethodHead || req.Method == http.MethodDelete {
		return ProxyModeSplice
	}
	cl := req.Headers.Get("Content-Length")
	if cl == "0" || cl == "" {
		return ProxyModeSplice
	}
	return ProxyModeBuffer
}

type ErrorCode int

const (
	ErrOK             ErrorCode = 0
	ErrBadRequest     ErrorCode = 10001
	ErrUnauthorized   ErrorCode = 10002
	ErrForbidden      ErrorCode = 10003
	ErrRateLimited    ErrorCode = 10004
	ErrCircuitOpen    ErrorCode = 10005
	ErrNoRoute        ErrorCode = 10006
	ErrBackendDown    ErrorCode = 10007
	ErrBackendTimeout ErrorCode = 10008
	ErrInternal       ErrorCode = 10009
)

type GatewayError struct {
	Code    ErrorCode
	Message string
	Detail  string
	Cause   error
}

func (e *GatewayError) Error() string {
	return fmt.Sprintf("[%d] %s: %s", e.Code, e.Message, e.Detail)
}

func (e *GatewayError) Unwrap() error {
	return e.Cause
}

func (e *GatewayError) HTTPStatus() int {
	switch e.Code {
	case ErrBadRequest:
		return http.StatusBadRequest
	case ErrUnauthorized:
		return http.StatusUnauthorized
	case ErrForbidden:
		return http.StatusForbidden
	case ErrRateLimited:
		return http.StatusTooManyRequests
	case ErrCircuitOpen:
		return http.StatusServiceUnavailable
	case ErrNoRoute:
		return http.StatusNotFound
	case ErrBackendDown:
		return http.StatusBadGateway
	case ErrBackendTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

func NewGatewayError(code ErrorCode, msg, detail string) *GatewayError {
	return &GatewayError{Code: code, Message: msg, Detail: detail}
}

func NewGatewayErrorWithCause(code ErrorCode, msg string, cause error) *GatewayError {
	return &GatewayError{Code: code, Message: msg, Detail: cause.Error(), Cause: cause}
}
