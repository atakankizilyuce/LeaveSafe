package eventlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/rotate"
)

func newLogger(t *testing.T, opts rotate.Options) (*Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := NewWithOptions(path, opts)
	if err != nil {
		t.Fatalf("open event log: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, path
}

func TestLogAndReadRoundTrip(t *testing.T) {
	l, path := newLogger(t, rotate.Options{})

	l.Log(Event{Type: EventArm, Message: "System armed"})
	l.Log(Event{Type: EventAlert, Sensor: "power", Message: "Charger unplugged"})

	events, err := ReadLast(path, 10)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("read %d events, want 2", len(events))
	}
	if events[0].Type != EventArm {
		t.Errorf("first event type = %q, want %q", events[0].Type, EventArm)
	}
	if events[1].Sensor != "power" || events[1].Message != "Charger unplugged" {
		t.Errorf("second event = %+v, want the power alert", events[1])
	}
}

func TestLogStampsTimeWhenMissing(t *testing.T) {
	l, path := newLogger(t, rotate.Options{})
	before := time.Now().Add(-time.Second)

	l.Log(Event{Type: EventArm})

	events, err := ReadLast(path, 1)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if events[0].Timestamp.Before(before) {
		t.Errorf("timestamp %v was not filled in", events[0].Timestamp)
	}
}

func TestLogPreservesExplicitTimestamp(t *testing.T) {
	l, path := newLogger(t, rotate.Options{})
	want := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	l.Log(Event{Type: EventDisarm, Timestamp: want})

	events, err := ReadLast(path, 1)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if !events[0].Timestamp.Equal(want) {
		t.Errorf("timestamp = %v, want %v", events[0].Timestamp, want)
	}
}

func TestReadLastReturnsNewestNOldestFirst(t *testing.T) {
	l, path := newLogger(t, rotate.Options{})
	for i := range 50 {
		l.Log(Event{Type: EventAlert, Message: fmt.Sprintf("event-%02d", i)})
	}

	events, err := ReadLast(path, 5)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("read %d events, want 5", len(events))
	}
	if events[0].Message != "event-45" {
		t.Errorf("oldest returned event = %q, want event-45", events[0].Message)
	}
	if events[4].Message != "event-49" {
		t.Errorf("newest returned event = %q, want event-49", events[4].Message)
	}
}

// The whole point of the tail read is that a file far larger than the request
// does not get pulled into memory — and that the answer stays correct when the
// records sit well past the first read window.
func TestReadLastCrossesTheReadWindow(t *testing.T) {
	l, path := newLogger(t, rotate.Options{MaxBytes: 1 << 30})

	padding := strings.Repeat("x", 200)
	const total = 400
	for i := range total {
		l.Log(Event{Type: EventAlert, Message: fmt.Sprintf("%s-%03d", padding, i)})
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() <= readWindow {
		t.Fatalf("test file is %d bytes, which does not exceed the %d byte window", info.Size(), readWindow)
	}

	events, err := ReadLast(path, 3)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("read %d events, want 3", len(events))
	}
	if !strings.HasSuffix(events[2].Message, "-399") {
		t.Errorf("newest event = %q, want the last one written", events[2].Message)
	}
}

func TestReadLastFallsBackToRotatedGenerations(t *testing.T) {
	// A tiny size limit forces several rotations, so the newest records live in
	// the live file and the rest only exist in the generations behind it.
	l, path := newLogger(t, rotate.Options{MaxBytes: 300, MaxFiles: 3})
	for i := range 30 {
		l.Log(Event{Type: EventAlert, Message: fmt.Sprintf("event-%02d", i)})
	}

	live, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read live file: %v", err)
	}
	if strings.Count(string(live), "\n") >= 10 {
		t.Fatalf("the live file already holds every record; rotation did not happen")
	}

	events, err := ReadLastWithFiles(path, 10, 3)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(events) != 10 {
		t.Fatalf("read %d events, want 10 spanning the rotated files", len(events))
	}
	if events[0].Message != "event-20" {
		t.Errorf("oldest returned event = %q, want event-20", events[0].Message)
	}
	if events[9].Message != "event-29" {
		t.Errorf("newest returned event = %q, want event-29", events[9].Message)
	}
}

func TestReadLastSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	content := `{"timestamp":"2026-01-01T00:00:00Z","type":"arm","message":"good one"}
this is not json at all
{"timestamp":"2026-01-01T00:00:01Z","type":"disarm","message":"good two"}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	events, err := ReadLast(path, 10)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("read %d events, want the 2 parseable ones", len(events))
	}
	if events[0].Message != "good one" || events[1].Message != "good two" {
		t.Errorf("read %q and %q, want both good records", events[0].Message, events[1].Message)
	}
}

// A process killed mid-write leaves a record without its newline. It must not
// take the readable history down with it.
func TestReadLastToleratesATornFinalRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	content := `{"timestamp":"2026-01-01T00:00:00Z","type":"arm","message":"complete"}
{"timestamp":"2026-01-01T00:00:01Z","type":"al`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	events, err := ReadLast(path, 10)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(events) != 1 || events[0].Message != "complete" {
		t.Errorf("read %+v, want just the complete record", events)
	}
}

func TestReadLastOnMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nothing-here.jsonl")
	if _, err := ReadLast(path, 5); err == nil {
		t.Error("reading a missing log returned no error")
	}
}

func TestReadLastOnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	events, err := ReadLast(path, 5)
	if err != nil {
		t.Fatalf("ReadLast: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("read %d events from an empty file, want 0", len(events))
	}
}

func TestRotationCapsTotalSize(t *testing.T) {
	l, path := newLogger(t, rotate.Options{MaxBytes: 512, MaxFiles: 2})
	for i := range 500 {
		l.Log(Event{Type: EventAlert, Message: fmt.Sprintf("event-%03d", i)})
	}

	var total int64
	for _, p := range append([]string{path}, rotate.Generations(path, 2)...) {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		total += info.Size()
	}

	// Three files of at most ~512 bytes each, plus slack for the record that
	// crosses the boundary. Without rotation this would be tens of kilobytes.
	if total > 3*512+512 {
		t.Errorf("log family grew to %d bytes despite rotation", total)
	}
}
