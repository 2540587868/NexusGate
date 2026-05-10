package proxy

import (
	"math/rand/v2"
	"time"
)

type RetryPolicy struct {
	MaxRetries       int
	RetryableStatus  []int
	Backoff          BackoffStrategy
}

func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:      2,
		RetryableStatus: []int{502, 503, 504},
		Backoff: &ExponentialBackoff{
			Base:   100 * time.Millisecond,
			Max:    5 * time.Second,
			Jitter: true,
		},
	}
}

type BackoffStrategy interface {
	Next(attempt int) time.Duration
}

type ExponentialBackoff struct {
	Base   time.Duration
	Max    time.Duration
	Jitter bool
}

func (eb *ExponentialBackoff) Next(attempt int) time.Duration {
	delay := eb.Base
	for i := 0; i < attempt; i++ {
		delay *= 2
		if delay > eb.Max {
			delay = eb.Max
			break
		}
	}

	if eb.Jitter {
		jitter := time.Duration(float64(delay) * (0.8 + rand.Float64()*0.4))
		if jitter > eb.Max {
			jitter = eb.Max
		}
		delay = jitter
	}

	return delay
}

type FixedBackoff struct {
	Interval time.Duration
}

func (fb *FixedBackoff) Next(attempt int) time.Duration {
	return fb.Interval
}

func IsRetryableStatus(statusCode int, policy *RetryPolicy) bool {
	if policy == nil {
		return false
	}
	for _, code := range policy.RetryableStatus {
		if statusCode == code {
			return true
		}
	}
	return false
}
