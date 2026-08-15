// Package endpoint tells anything running as the same user where this
// LeaveSafe is listening.
//
// The port is not knowable from outside the process. It defaults to zero, which
// means the operating system picks a free one at every start — so a client on
// the same machine has the pairing key sitting beside it in the config
// directory and no idea which port to spend it on. It either asks the person in
// front of it to read an address off a screen, or it probes, and neither is
// something a program should make somebody do about their own computer.
//
// So the address is written down, next to the key, for the length of the run.
// The file is the running program's, not the configuration's: it is created
// when the listener binds and removed when the program stops, and a stale one
// left by a crash is a port that refuses connections rather than a lie that
// keeps working.
//
// It is owner-only for the same reason the key file is. On a shared machine,
// which port the alarm is listening on is not everybody's business.
package endpoint

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileName is what the endpoint is written to, inside the application's config
// directory.
const FileName = "endpoint.json"

// fileMode keeps it readable only by its owner, as the key file beside it is.
const fileMode os.FileMode = 0o600

// Endpoint is where this run of the program can be reached, and enough about
// the run itself to tell a live one from something left behind.
type Endpoint struct {
	// Port is the one the listener actually bound, which is not necessarily the
	// one that was configured: a busy port is given up on for a free one
	// rather than refusing to start.
	Port int `json:"port"`

	// PID is the process holding it. Nothing in this program reads it — it is
	// for a person looking at the file and asking whether anything is still
	// there.
	PID int `json:"pid"`

	// Version is the build that wrote the file, for reading old ones later.
	Version string `json:"version,omitempty"`

	// StartedAt is when the listener bound.
	StartedAt time.Time `json:"started_at"`
}

// Path is where the endpoint file lives for a given config directory.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// Publish writes where this run is listening.
//
// Atomic, because a client reading a half-written file would get a port that
// was never bound and spend a connection attempt on it.
func Publish(dir string, e Endpoint) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("endpoint: preparing %s: %w", dir, err)
	}

	body, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("endpoint: encoding: %w", err)
	}

	tmp, err := os.CreateTemp(dir, FileName+".*")
	if err != nil {
		return fmt.Errorf("endpoint: creating a temporary file: %w", err)
	}
	name := tmp.Name()

	// Every failure from here removes the temporary file rather than leaving it
	// in the config directory for the rest of the installation's life.
	if err := writeAndClose(tmp, append(body, '\n')); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, fileMode); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("endpoint: setting permissions: %w", err)
	}
	if err := os.Rename(name, Path(dir)); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("endpoint: writing %s: %w", Path(dir), err)
	}
	return nil
}

func writeAndClose(f *os.File, body []byte) error {
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("endpoint: writing: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("endpoint: closing: %w", err)
	}
	return nil
}

// Read returns what is published, if anything is.
//
// A missing file is not an error: it means nothing is running, which is the
// ordinary answer on a machine where LeaveSafe is installed and not started.
func Read(dir string) (Endpoint, bool, error) {
	// #nosec G304 -- path is built from the app's own config dir, never user input
	body, err := os.ReadFile(Path(dir))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return Endpoint{}, false, nil
	case err != nil:
		return Endpoint{}, false, fmt.Errorf("endpoint: reading %s: %w", Path(dir), err)
	}

	e, ok := decode(body)
	if !ok {
		return Endpoint{}, false, nil
	}
	return e, true, nil
}

// decode reads an endpoint, and says whether there was one.
//
// The decoding error is deliberately dropped rather than returned. A file
// half-written by a version that did not rename into place, or edited by hand,
// is nothing to connect to — the same answer as no file at all — and a client
// that reported an outage over it would be reporting the wrong thing. The same
// goes for a port that cannot be one: there is no connection at the end of it.
func decode(body []byte) (Endpoint, bool) {
	var e Endpoint
	if err := json.Unmarshal(body, &e); err != nil {
		return Endpoint{}, false
	}
	if e.Port <= 0 || e.Port > 65535 {
		return Endpoint{}, false
	}
	return e, true
}

// Withdraw removes the file, on the way out.
//
// A missing one is not an error. Two paths lead here — a clean shutdown and a
// start that failed after publishing — and neither is worth a message about a
// file that is already gone.
func Withdraw(dir string) error {
	err := os.Remove(Path(dir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("endpoint: removing %s: %w", Path(dir), err)
	}
	return nil
}
