package endpoint

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// What follows is the filesystem saying no. Publishing is allowed to fail —
// the caller logs it and the alarm, the local network and every connected phone
// carry on — but it is not allowed to fail quietly, and it is not allowed to
// leave a temporary file in the directory a client reads. One left behind stays
// there for the life of the installation.

// namesIn is what the config directory holds, for tests that care that a
// failure left nothing behind.
func namesIn(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// failedWith checks that publishing failed, and said which step did.
func failedWith(t *testing.T, err error, step string) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s failed and publishing reported that it had worked", step)
	}
	if !strings.Contains(err.Error(), step) {
		t.Errorf("the error does not say that %s failed: %v", step, err)
	}
}

func TestAnEndpointThatCannotBeEncodedIsNotPublished(t *testing.T) {
	previous := marshalFn
	t.Cleanup(func() { marshalFn = previous })
	marshalFn = func(any, string, string) ([]byte, error) {
		return nil, errors.New("nothing to say")
	}

	dir := t.TempDir()
	failedWith(t, Publish(dir, Endpoint{Port: 9443}), "encoding")
	if names := namesIn(t, dir); len(names) != 0 {
		t.Errorf("the directory holds %v", names)
	}
}

func TestATemporaryFileThatCannotBeCreatedIsReported(t *testing.T) {
	// A config directory that has stopped being writable — someone else's
	// permissions, or a disk with nothing left on it.
	previous := createTempFn
	t.Cleanup(func() { createTempFn = previous })
	createTempFn = func(string, string) (*os.File, error) {
		return nil, errors.New("read-only file system")
	}

	dir := t.TempDir()
	failedWith(t, Publish(dir, Endpoint{Port: 9443}), "creating a temporary file")
	if names := namesIn(t, dir); len(names) != 0 {
		t.Errorf("the directory holds %v", names)
	}
}

func TestAWriteThatFailsTakesTheTemporaryFileWithIt(t *testing.T) {
	// The disk filling up between the create and the write. The file is handed
	// back already closed, which is what every write to it failing looks like
	// from inside Publish.
	previous := createTempFn
	t.Cleanup(func() { createTempFn = previous })
	createTempFn = func(dir, pattern string) (*os.File, error) {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		return f, nil
	}

	dir := t.TempDir()
	failedWith(t, Publish(dir, Endpoint{Port: 9443}), "writing")
	if names := namesIn(t, dir); len(names) != 0 {
		t.Errorf("a failed write left %v behind", names)
	}
}

func TestPermissionsThatCannotBeSetTakeTheTemporaryFileWithThem(t *testing.T) {
	// Owner-only is not a nicety here: it is the reason the file is safe to
	// write on a shared machine. A file that could not be restricted must not
	// be left where a client would read it.
	previous := chmodFn
	t.Cleanup(func() { chmodFn = previous })
	chmodFn = func(string, os.FileMode) error {
		return errors.New("not permitted")
	}

	dir := t.TempDir()
	failedWith(t, Publish(dir, Endpoint{Port: 9443}), "setting permissions")
	if names := namesIn(t, dir); len(names) != 0 {
		t.Errorf("a file nobody could restrict was left as %v", names)
	}
}

func TestARenameThatFailsTakesTheTemporaryFileWithIt(t *testing.T) {
	// A directory standing where the endpoint file goes. Nothing puts one
	// there, which is the point: the rename is the last step, and a failure in
	// it must still end with a directory holding no temporary files.
	dir := t.TempDir()
	inTheWay := Path(dir)
	if err := os.MkdirAll(filepath.Join(inTheWay, "occupied"), 0o700); err != nil {
		t.Fatal(err)
	}

	failedWith(t, Publish(dir, Endpoint{Port: 9443}), "writing")
	if names := namesIn(t, dir); len(names) != 1 || names[0] != FileName {
		t.Errorf("a failed rename left %v behind", names)
	}
}

// stuckCloser writes but will not close, the way a filesystem that buffers
// reports at the last moment that the bytes never landed.
type stuckCloser struct{ written int }

func (s *stuckCloser) Write(p []byte) (int, error) {
	s.written += len(p)
	return len(p), nil
}

func (*stuckCloser) Close() error { return errors.New("the disk had the last word") }

func TestACloseThatFailsAfterAGoodWriteIsStillAFailure(t *testing.T) {
	// The bytes were accepted and then lost. Reporting this as a success would
	// publish a port that was never written down.
	f := &stuckCloser{}
	err := writeAndClose(f, []byte("{}\n"))

	failedWith(t, err, "closing")
	if f.written == 0 {
		t.Error("the body was never written")
	}
}

func TestAnEndpointThatCannotBeReadIsAnError(t *testing.T) {
	// Not the same as nothing running, and not the same as nonsense in the
	// file. Something is at that path and this program cannot see it, which is
	// the one case a client should be told about rather than shown as "off".
	dir := t.TempDir()
	if err := os.MkdirAll(Path(dir), 0o700); err != nil {
		t.Fatal(err)
	}

	_, found, err := Read(dir)
	if err == nil {
		t.Fatal("a path that could not be read was reported as nothing running")
	}
	if found {
		t.Error("something unreadable was reported as a live endpoint")
	}
}

func TestWithdrawingSomethingThatWillNotGoIsReported(t *testing.T) {
	// A missing file is fine on the way out; a file that refuses to go is not,
	// because what is left behind outlives the program that wrote it.
	dir := t.TempDir()
	stuck := Path(dir)
	if err := os.MkdirAll(filepath.Join(stuck, "occupied"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := Withdraw(dir); err == nil {
		t.Error("a file that could not be removed was reported as gone")
	}
}
