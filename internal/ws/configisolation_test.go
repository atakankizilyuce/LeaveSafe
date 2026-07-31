package ws

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leavesafe/leavesafe/internal/config"
)

// underIsolatedRoot reports whether path sits inside the directory TestMain
// created for this package.
func underIsolatedRoot(t *testing.T, path string) bool {
	t.Helper()
	if isolatedConfigRoot == "" {
		t.Fatal("TestMain did not record an isolated config root")
	}
	root, err := filepath.Abs(isolatedConfigRoot)
	if err != nil {
		t.Fatalf("resolve the isolated root: %v", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	// Case-insensitive because Windows paths are, and this is a safety check
	// rather than a path-handling exercise.
	return strings.HasPrefix(strings.ToLower(abs), strings.ToLower(root))
}

// The guard on the guard: if the isolation in TestMain is ever removed, this is
// what says so, rather than a developer discovering it by losing their own
// configuration.
func TestTheConfigDirectoryIsIsolatedFromTheRealOne(t *testing.T) {
	if dir := config.ConfigDir(); !underIsolatedRoot(t, dir) {
		t.Fatalf("tests in this package would write to %s, which is the real config "+
			"directory on this machine — config.Save is reachable from the hub handlers "+
			"they call, and it renames over the file with no backup", dir)
	}
}

// And the thing that actually goes wrong: a hub handler saving configuration
// must land in the isolated directory, not in the one the developer runs
// LeaveSafe from.
func TestSavingConfigFromAHandlerStaysInsideTheIsolatedDirectory(t *testing.T) {
	hub := testHub(t)
	cfg := config.Default()
	hub.SetConfig(cfg)

	payload := configToPayload(cfg)
	payload.HeartbeatSeconds = 20
	payload.EnabledSensors = map[string]bool{"power": true}
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	// The handler is expected to have written something. Proving the write
	// happened is what makes the location assertion mean anything — without it
	// this passes just as well when nothing was saved at all.
	written := config.ConfigPath()
	if _, err := os.Stat(written); err != nil {
		t.Fatalf("update_config saved nothing, so this test proves nothing: %v", err)
	}
	if !underIsolatedRoot(t, written) {
		t.Errorf("update_config wrote to %s, outside the isolated directory", written)
	}
}
