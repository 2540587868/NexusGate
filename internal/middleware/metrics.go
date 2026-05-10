package middleware

import (
	"expvar"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var (
	requestTotal     atomic.Int64
	requestSuccess   atomic.Int64
	requestFailed    atomic.Int64
	requestDuration  sync.Map
	activeRequests   atomic.Int64

	backendRequests   sync.Map
	backendFailures   sync.Map
	backendLatency    sync.Map

	circuitBreakerOpen atomic.Int64
	rateLimitRejected  atomic.Int64
	totalDurationNanos atomic.Int64
)

func init() {
	expvar.Publish("nexusgate_requests_total", expvar.Func(func() interface{} {
		return requestTotal.Load()
	}))
	expvar.Publish("nexusgate_requests_success", expvar.Func(func() interface{} {
		return requestSuccess.Load()
	}))
	expvar.Publish("nexusgate_requests_failed", expvar.Func(func() interface{} {
		return requestFailed.Load()
	}))
	expvar.Publish("nexusgate_active_requests", expvar.Func(func() interface{} {
		return activeRequests.Load()
	}))
	expvar.Publish("nexusgate_circuit_breaker_open", expvar.Func(func() interface{} {
		return circuitBreakerOpen.Load()
	}))
	expvar.Publish("nexusgate_rate_limit_rejected", expvar.Func(func() interface{} {
		return rateLimitRejected.Load()
	}))
}

func RecordRequestStart() {
	requestTotal.Add(1)
	activeRequests.Add(1)
}

func RecordRequestEnd(success bool, duration time.Duration) {
	activeRequests.Add(-1)
	if success {
		requestSuccess.Add(1)
	} else {
		requestFailed.Add(1)
	}
	totalDurationNanos.Add(int64(duration))
	recordDuration(duration)
}

func RecordBackendRequest(backend string, success bool, duration time.Duration) {
	reqCounter := getOrInitCounter(&backendRequests, backend)
	reqCounter.Add(1)

	if !success {
		failCounter := getOrInitCounter(&backendFailures, backend)
		failCounter.Add(1)
	}

	latencyVal, _ := backendLatency.LoadOrStore(backend, &latencyTracker{})
	latencyVal.(*latencyTracker).record(duration)
}

func getOrInitCounter(m *sync.Map, key string) *atomic.Int64 {
	val, _ := m.LoadOrStore(key, &atomic.Int64{})
	return val.(*atomic.Int64)
}

func RecordCircuitBreakerOpen() {
	circuitBreakerOpen.Add(1)
}

func RecordRateLimitRejected() {
	rateLimitRejected.Add(1)
}

type latencyTracker struct {
	mu    sync.Mutex
	sum   time.Duration
	count int64
	min   time.Duration
	max   time.Duration
}

func (lt *latencyTracker) record(d time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.sum += d
	lt.count++
	if lt.min == 0 || d < lt.min {
		lt.min = d
	}
	if d > lt.max {
		lt.max = d
	}
}

func (lt *latencyTracker) stats() (avg, min, max time.Duration, count int64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if lt.count > 0 {
		avg = lt.sum / time.Duration(lt.count)
	}
	return avg, lt.min, lt.max, lt.count
}

var histogramBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}

func recordDuration(d time.Duration) {
	seconds := d.Seconds()
	for _, b := range histogramBuckets {
		if seconds <= b {
			getOrInitCounter(&requestDuration, fmt.Sprintf("%g", b)).Add(1)
		}
	}
	getOrInitCounter(&requestDuration, "+Inf").Add(1)
}

func MetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")

		fmt.Fprintf(w, "# HELP nexusgate_requests_total Total number of requests processed\n")
		fmt.Fprintf(w, "# TYPE nexusgate_requests_total counter\n")
		fmt.Fprintf(w, "nexusgate_requests_total %d\n\n", requestTotal.Load())

		fmt.Fprintf(w, "# HELP nexusgate_requests_success Total successful requests\n")
		fmt.Fprintf(w, "# TYPE nexusgate_requests_success counter\n")
		fmt.Fprintf(w, "nexusgate_requests_success %d\n\n", requestSuccess.Load())

		fmt.Fprintf(w, "# HELP nexusgate_requests_failed Total failed requests\n")
		fmt.Fprintf(w, "# TYPE nexusgate_requests_failed counter\n")
		fmt.Fprintf(w, "nexusgate_requests_failed %d\n\n", requestFailed.Load())

		fmt.Fprintf(w, "# HELP nexusgate_active_requests Currently active requests\n")
		fmt.Fprintf(w, "# TYPE nexusgate_active_requests gauge\n")
		fmt.Fprintf(w, "nexusgate_active_requests %d\n\n", activeRequests.Load())

		fmt.Fprintf(w, "# HELP nexusgate_circuit_breaker_open Total circuit breaker open events\n")
		fmt.Fprintf(w, "# TYPE nexusgate_circuit_breaker_open counter\n")
		fmt.Fprintf(w, "nexusgate_circuit_breaker_open %d\n\n", circuitBreakerOpen.Load())

		fmt.Fprintf(w, "# HELP nexusgate_rate_limit_rejected Total rate limit rejections\n")
		fmt.Fprintf(w, "# TYPE nexusgate_rate_limit_rejected counter\n")
		fmt.Fprintf(w, "nexusgate_rate_limit_rejected %d\n\n", rateLimitRejected.Load())

		fmt.Fprintf(w, "# HELP nexusgate_request_duration_seconds Request duration histogram\n")
		fmt.Fprintf(w, "# TYPE nexusgate_request_duration_seconds histogram\n")
		for _, b := range histogramBuckets {
			key := fmt.Sprintf("%g", b)
			val := int64(0)
			if v, ok := requestDuration.Load(key); ok {
				val = v.(*atomic.Int64).Load()
			}
			fmt.Fprintf(w, "nexusgate_request_duration_seconds_bucket{le=\"%g\"} %d\n", b, val)
		}
		infVal := int64(0)
		if v, ok := requestDuration.Load("+Inf"); ok {
			infVal = v.(*atomic.Int64).Load()
		}
		fmt.Fprintf(w, "nexusgate_request_duration_seconds_bucket{le=\"+Inf\"} %d\n", infVal)
		fmt.Fprintf(w, "nexusgate_request_duration_seconds_count %d\n", requestSuccess.Load()+requestFailed.Load())
		totalNanos := totalDurationNanos.Load()
		fmt.Fprintf(w, "nexusgate_request_duration_seconds_sum %f\n\n", float64(totalNanos)/1e9)

		fmt.Fprintf(w, "# HELP nexusgate_backend_requests_total Total requests per backend\n")
		fmt.Fprintf(w, "# TYPE nexusgate_backend_requests_total counter\n")
		backendRequests.Range(func(key, value interface{}) bool {
			fmt.Fprintf(w, "nexusgate_backend_requests_total{backend=\"%s\"} %d\n", key, value.(*atomic.Int64).Load())
			return true
		})
		fmt.Fprintln(w)

		fmt.Fprintf(w, "# HELP nexusgate_backend_failures_total Total failures per backend\n")
		fmt.Fprintf(w, "# TYPE nexusgate_backend_failures_total counter\n")
		backendFailures.Range(func(key, value interface{}) bool {
			fmt.Fprintf(w, "nexusgate_backend_failures_total{backend=\"%s\"} %d\n", key, value.(*atomic.Int64).Load())
			return true
		})
		fmt.Fprintln(w)

		fmt.Fprintf(w, "# HELP nexusgate_backend_latency_seconds Backend latency stats\n")
		fmt.Fprintf(w, "# TYPE nexusgate_backend_latency_seconds summary\n")
		backendLatency.Range(func(key, value interface{}) bool {
			avg, min, max, count := value.(*latencyTracker).stats()
			fmt.Fprintf(w, "nexusgate_backend_latency_seconds{backend=\"%s\",quantile=\"avg\"} %f\n", key, avg.Seconds())
			fmt.Fprintf(w, "nexusgate_backend_latency_seconds{backend=\"%s\",quantile=\"min\"} %f\n", key, min.Seconds())
			fmt.Fprintf(w, "nexusgate_backend_latency_seconds{backend=\"%s\",quantile=\"max\"} %f\n", key, max.Seconds())
			fmt.Fprintf(w, "nexusgate_backend_latency_seconds_count{backend=\"%s\"} %d\n", key, count)
			return true
		})
	}
}
