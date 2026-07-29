package eventlog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	log "github.com/sirupsen/logrus"
	"sync"
	"time"

	"github.com/leavesafe/leavesafe/internal/rotate"
)

// EventType represents the kind of event being logged.
type EventType string

const (
	EventArm        EventType = "arm"
	EventDisarm     EventType = "disarm"
	EventAlert      EventType = "alert"
	EventConnect    EventType = "connect"
	EventDisconnect EventType = "disconnect"
	EventAuthFail   EventType = "auth_fail"
	EventPinFail    EventType = "pin_fail"
	// EventPanic records a bug the process recovered from rather than died of.
	// It belongs in this log because it happened while the machine was supposed
	// to be watching itself, and the user deserves to see it next to the
	// arm/disarm history rather than only in a stack trace they never read.
	EventPanic EventType = "panic"
)

// readWindow is how many bytes of a file ReadLast pulls in on its first pass.
// The window doubles until enough records are found or the whole file has been
// read, so a request for the last twenty events touches a few kilobytes rather
// than however many months of history have accumulated.
const readWindow int64 = 16 << 10 // 16 KiB

// Event represents a single logged event.
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Type      EventType `json:"type"`
	Sensor    string    `json:"sensor,omitempty"`
	Message   string    `json:"message,omitempty"`
}

// Logger writes events to a size-rotated JSONL file.
type Logger struct {
	mu       sync.Mutex
	writer   *rotate.Writer
	maxFiles int
}

// New creates a new event logger appending to path under the default rotation
// policy. The parent directory must exist.
func New(path string) (*Logger, error) {
	return NewWithOptions(path, rotate.Options{})
}

// NewWithOptions creates an event logger with an explicit rotation policy.
func NewWithOptions(path string, opts rotate.Options) (*Logger, error) {
	w, err := rotate.Open(path, opts)
	if err != nil {
		return nil, err
	}
	maxFiles := opts.MaxFiles
	if maxFiles == 0 {
		maxFiles = rotate.DefaultMaxFiles
	}
	return &Logger{writer: w, maxFiles: maxFiles}, nil
}

// Path returns the path of the live log file.
func (l *Logger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writer.Path()
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
		log.Warnf("Failed to marshal event: %v", err)
		return
	}
	if _, err := l.writer.Write(append(data, '\n')); err != nil {
		log.Warnf("Failed to write event log: %v", err)
	}
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writer.Close()
}

// ReadLast returns the most recent n events for path, oldest first.
//
// It reads backwards from the end of the live file and, if that does not hold n
// records, continues into the rotated generations. The whole file is never
// loaded: history that has been running for months would otherwise be pulled
// into memory to answer a request for the last twenty lines.
func ReadLast(path string, n int) ([]Event, error) {
	return readLast(path, n, rotate.DefaultMaxFiles)
}

// ReadLastWithFiles is ReadLast for a logger configured with a non-default
// number of rotated generations.
func ReadLastWithFiles(path string, n, maxFiles int) ([]Event, error) {
	return readLast(path, n, maxFiles)
}

func readLast(path string, n, maxFiles int) ([]Event, error) {
	if n <= 0 {
		n = 1
	}

	events, err := tailEvents(path, n)
	if err != nil {
		return nil, err
	}

	// Walk back through the rotated generations only when the live file did not
	// hold enough. Newest generation first, and each batch goes in front of
	// what we already have.
	for _, gen := range rotate.Generations(path, maxFiles) {
		if len(events) >= n {
			break
		}
		older, err := tailEvents(gen, n-len(events))
		if err != nil {
			// A generation that cannot be read is not worth failing the whole
			// request for: the newer records are still valid history.
			log.Debugf("Skipping unreadable event log generation %s: %v", gen, err)
			continue
		}
		events = append(older, events...)
	}

	if len(events) > n {
		events = events[len(events)-n:]
	}
	return events, nil
}

// tailEvents returns up to n events from the end of path, oldest first.
func tailEvents(path string, n int) ([]Event, error) {
	// #nosec G304 -- path is built from the app's own config dir, never user input
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()

	for window := readWindow; ; window *= 2 {
		if window > size {
			window = size
		}
		events, err := parseTail(f, size, window)
		if err != nil {
			return nil, err
		}
		// Enough records, or we have already read the whole file and there are
		// no more to find.
		if len(events) >= n || window >= size {
			if len(events) > n {
				events = events[len(events)-n:]
			}
			return events, nil
		}
	}
}

// parseTail reads the final window bytes of f and decodes the complete JSONL
// records it contains, oldest first.
func parseTail(f *os.File, size, window int64) ([]Event, error) {
	offset := size - window
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek: %w", err)
	}
	data := make([]byte, window)
	read, err := io.ReadFull(f, data)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, fmt.Errorf("read: %w", err)
	}
	data = data[:read]

	lines := splitLines(data)
	// Starting mid-file almost always lands inside a record. That first
	// fragment is not valid JSON and must be dropped rather than guessed at.
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:]
	}

	events := make([]Event, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			// A torn write at the tail of the file, or a line from a version
			// that wrote a different shape. Skip it; the rest is still history.
			continue
		}
		events = append(events, ev)
	}
	return events, nil
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
