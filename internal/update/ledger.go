package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ledgerFile is the bookkeeping file kept alongside the config.
const ledgerFile = "update.json"

// Record is what one run's update checking tells the next.
//
// This is kept apart from internal/state deliberately. That package is about one
// thing — whether the machine was armed when it last stopped — and folding
// unrelated bookkeeping into it would blur a boundary worth keeping.
type Record struct {
	// LastCheck is when a check was last attempted, successful or not. A copy
	// caught in a crash loop must not query GitHub on every start.
	LastCheck time.Time `json:"last_check,omitzero"`
	// LastSuccess is when a check last completed without error.
	LastSuccess time.Time `json:"last_success,omitzero"`
	// LastSeenLatest is the newest version already reported to the user, so
	// someone who has decided to stay put is told once per release rather than
	// once per interval.
	LastSeenLatest string `json:"last_seen_latest,omitempty"`
}

// Ledger persists a Record to a file. The zero value is not usable; call NewLedger.
type Ledger struct {
	mu   sync.Mutex
	path string
}

// NewLedger returns a ledger writing to dir/update.json.
func NewLedger(dir string) *Ledger {
	return &Ledger{path: filepath.Join(dir, ledgerFile)}
}

// Path returns the ledger file's location.
func (l *Ledger) Path() string { return l.path }

// Load reads the recorded bookkeeping.
//
// A missing file means nothing has ever been checked. So does a file that does
// not parse: the worst this costs is one extra query, and there is nothing here
// worth preserving by moving a corrupt file aside the way the config is.
func (l *Ledger) Load() Record {
	l.mu.Lock()
	defer l.mu.Unlock()

	// #nosec G304 -- path is built from the app's own config dir, never user input
	data, err := os.ReadFile(l.path)
	if err != nil {
		return Record{}
	}

	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}
	}
	return rec
}

// Save records rec, replacing whatever was there.
//
// The write goes to a temporary file that is then renamed over the real one, so
// a crash midway through leaves the previous record intact rather than a
// half-written file.
func (l *Ledger) Save(rec Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode update record: %w", err)
	}

	dir := filepath.Dir(l.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ledgerFile+".*")
	if err != nil {
		return fmt.Errorf("create temp update record: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Only fires on the failure paths; a successful call has already renamed
		// the file away by the time it gets here.
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp update record: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp update record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp update record: %w", err)
	}
	if err := os.Rename(tmpName, l.path); err != nil {
		return fmt.Errorf("replace update record: %w", err)
	}
	return nil
}
