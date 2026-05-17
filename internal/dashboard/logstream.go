package dashboard

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type LogEntry struct {
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Source    string `json:"source,omitempty"`
}

type LogSubscriber struct {
	ch       chan LogEntry
	filters  map[string]string
}

type LogStream struct {
	mu          sync.RWMutex
	subscribers map[*LogSubscriber]struct{}
	buffer      []LogEntry
	maxBuffer   int
}

var globalLogStream = &LogStream{
	subscribers: make(map[*LogSubscriber]struct{}),
	buffer:      make([]LogEntry, 0, 200),
	maxBuffer:   200,
}

func GetLogStream() *LogStream {
	return globalLogStream
}

func (ls *LogStream) Subscribe(filters map[string]string) *LogSubscriber {
	sub := &LogSubscriber{
		ch:      make(chan LogEntry, 64),
		filters: filters,
	}

	ls.mu.Lock()
	ls.subscribers[sub] = struct{}{}
	ls.mu.Unlock()

	return sub
}

func (ls *LogStream) Unsubscribe(sub *LogSubscriber) {
	ls.mu.Lock()
	delete(ls.subscribers, sub)
	ls.mu.Unlock()

	close(sub.ch)
}

func (ls *LogStream) Publish(entry LogEntry) {
	ls.mu.Lock()
	if len(ls.buffer) >= ls.maxBuffer {
		ls.buffer = ls.buffer[1:]
	}
	ls.buffer = append(ls.buffer, entry)

	subs := make([]*LogSubscriber, 0, len(ls.subscribers))
	for sub := range ls.subscribers {
		subs = append(subs, sub)
	}
	ls.mu.Unlock()

	for _, sub := range subs {
		if !matchesFilter(entry, sub.filters) {
			continue
		}
		select {
		case sub.ch <- entry:
		default:
		}
	}
}

func (ls *LogStream) Recent() []LogEntry {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	result := make([]LogEntry, len(ls.buffer))
	copy(result, ls.buffer)
	return result
}

func matchesFilter(entry LogEntry, filters map[string]string) bool {
	if len(filters) == 0 {
		return true
	}
	if level, ok := filters["level"]; ok && level != "" && entry.Level != level {
		return false
	}
	if source, ok := filters["source"]; ok && source != "" && entry.Source != source {
		return false
	}
	return true
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	filters := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			filters[k] = v[0]
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	stream := GetLogStream()

	recent := stream.Recent()
	for _, entry := range recent {
		if !matchesFilter(entry, filters) {
			continue
		}
		data, _ := json.Marshal(entry)
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	flusher.Flush()

	sub := stream.Subscribe(filters)
	defer stream.Unsubscribe(sub)

	slog.Info("SSE client connected", "remote", r.RemoteAddr)

	done := r.Context().Done()
	for {
		select {
		case entry, ok := <-sub.ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(entry)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-done:
			slog.Info("SSE client disconnected", "remote", r.RemoteAddr)
			return
		case <-time.After(s.sseKeepaliveInterval):
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
