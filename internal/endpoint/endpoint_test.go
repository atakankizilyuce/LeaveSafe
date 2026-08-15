package endpoint

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// What this file has to get right is not the JSON. It is that a client on the
// same machine can tell "here is where it is listening" from "nothing is
// running" and from "something wrote nonsense here" — because the first is a
// connection, and the other two are a row on a screen that must not claim one.

func TestPublishingSaysWhereTheListenerBound(t *testing.T) {
	dir := t.TempDir()
	started := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)

	if err := Publish(dir, Endpoint{
		Port:      54321,
		PID:       4242,
		Version:   "1.3.1",
		StartedAt: started,
	}); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	got, found, err := Read(dir)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if !found {
		t.Fatal("nothing was published")
	}
	if got.Port != 54321 || got.PID != 4242 || got.Version != "1.3.1" {
		t.Errorf("got %+v", got)
	}
	if !got.StartedAt.Equal(started) {
		t.Errorf("started at %s, not %s", got.StartedAt, started)
	}
}

func TestNothingRunningIsNotAFailure(t *testing.T) {
	// The ordinary answer on a machine where LeaveSafe is installed and has not
	// been started. A client asking should get "no" rather than an error it has
	// to decide how to show.
	_, found, err := Read(t.TempDir())
	if err != nil {
		t.Fatalf("an empty config directory was an error: %v", err)
	}
	if found {
		t.Error("something was found in an empty directory")
	}
}

func TestSomethingUnreadableIsReportedAsNothingRunning(t *testing.T) {
	// A file half-written by a version that did not rename into place, or
	// edited by hand. There is nothing here to connect to either way, and a
	// client that treated it as an outage would be reporting the wrong thing.
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("{not json"), fileMode); err != nil {
		t.Fatal(err)
	}

	_, found, err := Read(dir)
	if err != nil {
		t.Errorf("unreadable was an error: %v", err)
	}
	if found {
		t.Error("unreadable was reported as a live endpoint")
	}
}

func TestAPortThatCannotBeOneIsNotAnEndpoint(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		dir := t.TempDir()
		if err := Publish(dir, Endpoint{Port: port}); err != nil {
			t.Fatal(err)
		}

		if _, found, _ := Read(dir); found {
			t.Errorf("port %d was accepted as somewhere to connect", port)
		}
	}
}

func TestTheFileIsOwnerOnly(t *testing.T) {
	// The same rule the key file beside it follows. On a shared machine, which
	// port the alarm is listening on is not everybody's business.
	if runtime.GOOS == "windows" {
		t.Skip("file modes on Windows are not the POSIX ones this asserts")
	}

	dir := t.TempDir()
	if err := Publish(dir, Endpoint{Port: 1}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fileMode {
		t.Errorf("mode is %v, not %v", info.Mode().Perm(), fileMode)
	}
}

func TestPublishingAgainReplacesWhatWasThere(t *testing.T) {
	// A restart binds a different port. The file has to say the new one rather
	// than the one that is no longer listening.
	dir := t.TempDir()
	if err := Publish(dir, Endpoint{Port: 1111}); err != nil {
		t.Fatal(err)
	}
	if err := Publish(dir, Endpoint{Port: 2222}); err != nil {
		t.Fatal(err)
	}

	got, _, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2222 {
		t.Errorf("port is %d, not the one bound last", got.Port)
	}
}

func TestPublishingLeavesNothingBehindWhenItWorks(t *testing.T) {
	// The temporary file is created in the config directory, so a failure to
	// clean up would litter somebody's application data for the life of the
	// installation.
	dir := t.TempDir()
	if err := Publish(dir, Endpoint{Port: 1}); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the directory holds %v", names)
	}
}

func TestPublishingCreatesTheDirectoryItNeeds(t *testing.T) {
	// The first run of a fresh installation. Nothing has written to the config
	// directory yet.
	dir := filepath.Join(t.TempDir(), "LeaveSafe")

	if err := Publish(dir, Endpoint{Port: 9443}); err != nil {
		t.Fatalf("publishing into a directory that did not exist: %v", err)
	}
	if _, found, _ := Read(dir); !found {
		t.Error("nothing was published")
	}
}

func TestWithdrawingTakesItAway(t *testing.T) {
	dir := t.TempDir()
	if err := Publish(dir, Endpoint{Port: 9443}); err != nil {
		t.Fatal(err)
	}

	if err := Withdraw(dir); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}

	if _, found, _ := Read(dir); found {
		t.Error("it is still published after the program stopped")
	}
}

func TestWithdrawingNothingIsNotAFailure(t *testing.T) {
	// Two paths lead here: a clean shutdown, and a start that failed before it
	// ever published. Neither is worth a message about a file already gone.
	if err := Withdraw(t.TempDir()); err != nil {
		t.Errorf("withdrawing nothing: %v", err)
	}
}

func TestPublishingWhereItCannotWriteSaysSo(t *testing.T) {
	// A path that is a file rather than a directory. The caller logs this and
	// carries on — the alarm still works, it is only harder to find.
	dir := t.TempDir()
	notADir := filepath.Join(dir, "in-the-way")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Publish(filepath.Join(notADir, "under"), Endpoint{Port: 1}); err == nil {
		t.Error("writing under a file was reported as having worked")
	}
}
