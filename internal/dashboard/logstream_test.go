package dashboard

import (
	"testing"
	"time"
)

func TestLogStreamPublish(t *testing.T) {
	ls := &LogStream{
		subscribers: make(map[*LogSubscriber]struct{}),
		buffer:      make([]LogEntry, 0, 200),
		maxBuffer:   200,
	}

	sub := ls.Subscribe(nil)

	entry := LogEntry{
		Level:     "info",
		Message:   "test message",
		Timestamp: time.Now().Format(time.RFC3339),
		Source:    "test",
	}

	ls.Publish(entry)

	select {
	case received := <-sub.ch:
		if received.Level != entry.Level {
			t.Errorf("Level = %q, want %q", received.Level, entry.Level)
		}
		if received.Message != entry.Message {
			t.Errorf("Message = %q, want %q", received.Message, entry.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published entry")
	}

	ls.Unsubscribe(sub)
}

func TestLogStreamRecent(t *testing.T) {
	ls := &LogStream{
		subscribers: make(map[*LogSubscriber]struct{}),
		buffer:      make([]LogEntry, 0, 200),
		maxBuffer:   200,
	}

	entries := []LogEntry{
		{Level: "info", Message: "first", Timestamp: time.Now().Format(time.RFC3339)},
		{Level: "warn", Message: "second", Timestamp: time.Now().Format(time.RFC3339)},
		{Level: "error", Message: "third", Timestamp: time.Now().Format(time.RFC3339)},
	}

	for _, e := range entries {
		ls.Publish(e)
	}

	recent := ls.Recent()
	if len(recent) != 3 {
		t.Fatalf("len(Recent()) = %d, want 3", len(recent))
	}
	if recent[0].Message != "first" {
		t.Errorf("recent[0].Message = %q, want %q", recent[0].Message, "first")
	}
	if recent[1].Message != "second" {
		t.Errorf("recent[1].Message = %q, want %q", recent[1].Message, "second")
	}
	if recent[2].Message != "third" {
		t.Errorf("recent[2].Message = %q, want %q", recent[2].Message, "third")
	}
}

func TestLogStreamUnsubscribe(t *testing.T) {
	ls := &LogStream{
		subscribers: make(map[*LogSubscriber]struct{}),
		buffer:      make([]LogEntry, 0, 200),
		maxBuffer:   200,
	}

	sub := ls.Subscribe(nil)
	ls.Unsubscribe(sub)

	_, ok := <-sub.ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestLogStreamBufferLimit(t *testing.T) {
	maxBuffer := 5
	ls := &LogStream{
		subscribers: make(map[*LogSubscriber]struct{}),
		buffer:      make([]LogEntry, 0, maxBuffer),
		maxBuffer:   maxBuffer,
	}

	for i := 0; i < 10; i++ {
		ls.Publish(LogEntry{
			Level:     "info",
			Message:   "msg",
			Timestamp: time.Now().Format(time.RFC3339),
		})
	}

	recent := ls.Recent()
	if len(recent) != maxBuffer {
		t.Errorf("len(Recent()) = %d, want %d", len(recent), maxBuffer)
	}
}

func TestLogStreamFilter(t *testing.T) {
	ls := &LogStream{
		subscribers: make(map[*LogSubscriber]struct{}),
		buffer:      make([]LogEntry, 0, 200),
		maxBuffer:   200,
	}

	sub := ls.Subscribe(map[string]string{"level": "error"})

	ls.Publish(LogEntry{Level: "info", Message: "info msg", Timestamp: time.Now().Format(time.RFC3339)})
	ls.Publish(LogEntry{Level: "error", Message: "error msg", Timestamp: time.Now().Format(time.RFC3339)})

	select {
	case received := <-sub.ch:
		if received.Level != "error" {
			t.Errorf("received Level = %q, want %q", received.Level, "error")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for filtered entry")
	}

	select {
	case <-sub.ch:
		t.Error("should not receive non-matching entry")
	case <-time.After(50 * time.Millisecond):
	}

	ls.Unsubscribe(sub)
}

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name    string
		entry   LogEntry
		filters map[string]string
		want    bool
	}{
		{
			name:    "empty filters match all",
			entry:   LogEntry{Level: "info", Source: "svc"},
			filters: map[string]string{},
			want:    true,
		},
		{
			name:    "nil filters match all",
			entry:   LogEntry{Level: "info", Source: "svc"},
			filters: nil,
			want:    true,
		},
		{
			name:    "matching level filter",
			entry:   LogEntry{Level: "error", Source: "svc"},
			filters: map[string]string{"level": "error"},
			want:    true,
		},
		{
			name:    "non-matching level filter",
			entry:   LogEntry{Level: "info", Source: "svc"},
			filters: map[string]string{"level": "error"},
			want:    false,
		},
		{
			name:    "matching source filter",
			entry:   LogEntry{Level: "info", Source: "gateway"},
			filters: map[string]string{"source": "gateway"},
			want:    true,
		},
		{
			name:    "non-matching source filter",
			entry:   LogEntry{Level: "info", Source: "proxy"},
			filters: map[string]string{"source": "gateway"},
			want:    false,
		},
		{
			name:    "both filters match",
			entry:   LogEntry{Level: "warn", Source: "router"},
			filters: map[string]string{"level": "warn", "source": "router"},
			want:    true,
		},
		{
			name:    "level matches source does not",
			entry:   LogEntry{Level: "warn", Source: "proxy"},
			filters: map[string]string{"level": "warn", "source": "router"},
			want:    false,
		},
		{
			name:    "empty level filter value matches all",
			entry:   LogEntry{Level: "debug", Source: "svc"},
			filters: map[string]string{"level": ""},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesFilter(tt.entry, tt.filters)
			if got != tt.want {
				t.Errorf("matchesFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogStreamGlobalInstance(t *testing.T) {
	a := GetLogStream()
	b := GetLogStream()
	if a != b {
		t.Error("GetLogStream() should return the same instance")
	}
}
