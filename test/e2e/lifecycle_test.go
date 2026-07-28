//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/test/harness"
)

// TestApp_StartsAndServes proves the binary boots on this OS and accepts TCP.
func TestApp_StartsAndServes(t *testing.T) {
	app := harness.Start(t, harness.Options{})

	conn, err := net.DialTimeout("tcp",
		fmt.Sprintf("127.0.0.1:%d", app.Port()), 5*time.Second)
	if err != nil {
		t.Fatalf("app is not accepting connections on port %d: %v", app.Port(), err)
	}
	_ = conn.Close()
}

// TestApp_PublishesPairingKey proves a usable pairing key reaches the operator.
func TestApp_PublishesPairingKey(t *testing.T) {
	app := harness.Start(t, harness.Options{})

	key := app.Key()
	if len(key) != 19 {
		t.Fatalf("pairing key %q has length %d, want 19 (XXXX-XXXX-XXXX-XXXX)", key, len(key))
	}
	for _, part := range strings.Split(key, "-") {
		if len(part) != 4 {
			t.Fatalf("pairing key %q is not four groups of four digits", key)
		}
	}
}

// TestApp_ShutsDownCleanly proves the process exits on a termination signal
// and releases its port, so a restart is never blocked by a zombie.
func TestApp_ShutsDownCleanly(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	port := app.Port()

	if err := app.Stop(); err != nil {
		t.Fatalf("app did not shut down cleanly: %v\n--- output ---\n%s", err, app.Output())
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port %d was not released after shutdown: %v", port, err)
	}
	_ = ln.Close()
}

// TestApp_WritesEventLog proves the audit trail is created in the config dir
// and holds well-formed JSONL.
func TestApp_WritesEventLog(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	path := filepath.Join(app.ConfigDir(), "events.jsonl")

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("event log was not created at %s: %v", path, err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is built by the harness
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("event log line %d is not valid JSON: %v (%q)", i+1, err, line)
		}
	}
}
