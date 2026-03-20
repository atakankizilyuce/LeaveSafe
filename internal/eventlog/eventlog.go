package eventlog

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// EventType represents the kind of event being logged.
type EventType string

const (
	EventArm        EventType = "arm"
	EventDisarm     EventType = "disarm"
	EventAlert      EventType = "alert"
	EventConnect    EventType = "connect"
	EventDisconnect EventType = "disconnect"
)

// Event represents a single logged event.
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Type      EventType `json:"type"`
	Sensor    string    `json:"sensor,omitempty"`
	Message   string    `json:"message,omitempty"`
}

// Logger writes events to a JSONL file.
type Logger struct {
	mu   sync.Mutex
	file *os.File
}

// New creates a new event logger that appends to the given file path.
// The parent directory must exist.
func New(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Logger{file: f}, nil
}

// Log writes an event to the log file.
func (l *Logger) Log(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	l.file.Write(append(data, '\n'))
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}

// ReadLast reads the last n events from a JSONL file.
func ReadLast(path string, n int) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var all []Event
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		all = append(all, ev)
	}

	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all, nil
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
