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
	// Port is the one the last run's listener bound.
	//
	// A phone is given an address to dial — an address it keeps, because the
	// whole point of pairing is that it works again tomorrow without anybody
	// standing at the machine. With no port configured the operating system
	// picks a free one at every start, so that address stopped being true the
	// first time the program was restarted: the phone retried a dead port for
	// ever, and pairing again left a second entry beside the first, both named
	// after the same machine.
	//
	// So the address is remembered rather than reinvented. It is a preference
	// and not a claim — a port somebody else has taken since is given up for a
	// free one exactly as before.
	Port int `json:"port,omitempty"`
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
	return s.load()
}

// load is Load with the lock already held, for the writers below: both of them
// change one field and have to leave the rest of the file as they found it.
func (s *Store) load() (State, error) {
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

	// What is already there, so that arming does not throw away the port the
	// last run bound. A file that cannot be read is treated as an empty one:
	// this is a write, and refusing to record that the machine is now watching
	// because of an unreadable old file would lose the more important fact.
	prev, _ := s.load()
	return s.write(State{
		Armed:     armed,
		ChangedAt: time.Now(),
		Version:   s.version,
		Port:      prev.Port,
	})
}

// SavePort records where the listener bound, for the next run to try first.
//
// Kept apart from the armed state and from ChangedAt, which is when the arming
// changed and not when this was written.
func (s *Store) SavePort(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, _ := s.load()
	if prev.Port == port {
		return nil
	}
	prev.Port = port
	prev.Version = s.version
	return s.write(prev)
}

// write puts a whole state on disk, with the lock already held.
func (s *Store) write(st State) error {
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
