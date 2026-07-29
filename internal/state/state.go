// Package state records whether the machine was armed, so that fact survives
// the process that knew it.
//
// Without this, a LeaveSafe that dies — a crash, a battery running out, a
// reboot, or someone closing the window precisely because it was watching them
// — comes back up disarmed and says nothing. The user returns to a laptop that
// looks exactly like one that was never disturbed. That gap is the failure this
// package exists to make visible.
//
// Restoring the armed state automatically is a separate, opt-in decision: see
// Config.RestoreArmedState. Warning about it is not, because a silent gap in
// monitoring is worse than a noisy one.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// fileName is the state file kept alongside the config.
const fileName = "state.json"

// State is what one run of the program tells the next.
type State struct {
	// Armed is whether monitoring was active when this file was last written.
	Armed bool `json:"armed"`
	// ChangedAt is when Armed last changed.
	ChangedAt time.Time `json:"changed_at"`
	// Version is the build that wrote the file, for reading old files later.
	Version string `json:"version,omitempty"`
}

// Store persists State to a file. The zero value is not usable; call NewStore.
type Store struct {
	mu      sync.Mutex
	path    string
	version string
}

// NewStore returns a store writing to dir/state.json. version is recorded in
// the file for diagnostics.
func NewStore(dir, version string) *Store {
	return &Store{path: filepath.Join(dir, fileName), version: version}
}

// Path returns the state file's location.
func (s *Store) Path() string { return s.path }

// Load reads the recorded state. A missing file is not an error: it means this
// is the first run, which is indistinguishable from a clean disarmed state.
func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// #nosec G304 -- path is built from the app's own config dir, never user input
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read state: %w", err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		// A truncated state file is a fact about the last run, not a reason to
		// refuse to start. Report it and treat the state as unknown.
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	return st, nil
}

// Save records armed as the current state.
//
// The write goes to a temporary file that is then renamed over the real one, so
// a crash midway through leaves the previous state intact rather than a
// half-written file that parses as "not armed".
func (s *Store) Save(armed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := State{Armed: armed, ChangedAt: time.Now(), Version: s.version}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, fileName+".*")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Only fires on the failure paths; the rename below has already moved
		// the file away by the time a successful call gets here.
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp state: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp state: %w", err)
	}
	// Without the sync, a power loss right after the rename can leave the
	// directory entry pointing at a file whose contents never reached the disk
	// — which is exactly the crash this file is meant to survive.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

// Clear removes the state file.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
