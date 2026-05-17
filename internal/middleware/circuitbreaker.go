package middleware

import (
	"sync"
	"time"

	"github.com/nexusgate/nexusgate/internal/gateway"
)

type State int

const (
	StateClosed    State = iota
	StateOpen
	StateHalfOpen
)

type CircuitBreaker struct {
	mu               sync.Mutex
	state            State
	failureCount     int
	successCount     int
	failureThreshold int
	successThreshold int
	timeout          time.Duration
	lastFailure      time.Time
	halfOpenActive   int
}

func NewCircuitBreaker(failureThreshold, successThreshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
	}
}

func CircuitBreakerMiddleware(cb *CircuitBreaker) gateway.Middleware {
	return func(next gateway.Handler) gateway.Handler {
		return func(req *gateway.Request) (*gateway.Response, error) {
			if !cb.Allow() {
				return nil, gateway.NewGatewayError(gateway.ErrCircuitOpen,
					"circuit breaker is open", "too many failures, requests are blocked")
			}

			resp, err := next(req)
			if err != nil {
				cb.RecordFailure()
				return resp, err
			}

			if resp.StatusCode >= 500 {
				cb.RecordFailure()
			} else {
				cb.RecordSuccess()
			}

			return resp, nil
		}
	}
}

func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.state = StateHalfOpen
			cb.successCount = 0
			cb.halfOpenActive = 1
			return true
		}
		return false
	case StateHalfOpen:
		if cb.halfOpenActive < cb.successThreshold {
			cb.halfOpenActive++
			return true
		}
		return false
	default:
		return true
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailure = time.Now()

	if cb.state == StateHalfOpen {
		cb.state = StateOpen
		cb.failureCount = 0
		cb.halfOpenActive = 0
		RecordCircuitBreakerOpen()
		return
	}

	if cb.failureCount >= cb.failureThreshold {
		cb.state = StateOpen
		RecordCircuitBreakerOpen()
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		cb.successCount++
		cb.halfOpenActive--
		if cb.halfOpenActive < 0 {
			cb.halfOpenActive = 0
		}
		if cb.successCount >= cb.successThreshold {
			cb.state = StateClosed
			cb.successCount = 0
			cb.halfOpenActive = 0
			cb.failureCount = 0
		}
	} else {
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.failureCount = 0
	cb.successCount = 0
	cb.halfOpenActive = 0
	cb.lastFailure = time.Time{}
}

func (cb *CircuitBreaker) UpdateConfig(failureThreshold, successThreshold int, timeout time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failureThreshold = failureThreshold
	cb.successThreshold = successThreshold
	cb.timeout = timeout
}
