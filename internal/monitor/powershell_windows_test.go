//go:build windows

package monitor

import (
	"context"
	"testing"
	"time"
)

// A probe that never answers must not become a probe that never returns.
//
// This is the failure that matters: sensor availability is checked while the
// sensors are registered, and that happens before the server binds. An
// unbounded WMI query there does not merely make one sensor slow — it holds up
// the moment the phone can reach the laptop at all.
func TestPowerShellProbeGivesUp(t *testing.T) {
	start := time.Now()
	_, err := powershellOutput(context.Background(), pollTimeout, "Start-Sleep -Seconds 60")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("a probe that sleeps for a minute returned without an error")
	}
	// Generous slack: a CI runner starting a PowerShell process is not fast.
	if limit := pollTimeout + 20*time.Second; elapsed > limit {
		t.Errorf("the probe took %v to give up, past the %v timeout by more than the slack", elapsed, limit)
	}
}

// Canceling the sensor's context has to stop a probe in flight, or disarming
// would wait on a query nobody is interested in any more.
func TestPowerShellProbeHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := powershellOutput(ctx, pollTimeout, "Start-Sleep -Seconds 60")
	elapsed := time.Since(start)

	if err == nil {
		t.Error("a canceled probe returned without an error")
	}
	if elapsed >= pollTimeout {
		t.Errorf("canceling took %v; it should return well before the %v timeout", elapsed, pollTimeout)
	}
}

// Asking whether a sensor can work here must cost one process, not one per
// heartbeat.
//
// Available is called from every status broadcast — every fifteen seconds for
// as long as the program runs — and on Windows it answers by starting
// PowerShell and asking WMI. The answer cannot change while the program is
// running, so it is worked out once. Before the cache, an armed laptop spawned
// a PowerShell process four times a minute to re-ask a settled question.
func TestLidAvailabilityIsAskedOnce(t *testing.T) {
	s := NewLidSensor()

	start := time.Now()
	first := s.Available()
	firstTook := time.Since(start)

	start = time.Now()
	for range 50 {
		if s.Available() != first {
			t.Fatal("the availability answer changed between calls")
		}
	}
	rest := time.Since(start)

	// Fifty cached answers must be far quicker than the one that did the work.
	// Comparing against the first call rather than a fixed number keeps this
	// meaningful on a slow machine, where the real query is slower too.
	if rest > firstTook {
		t.Errorf("fifty repeat calls took %v, longer than the first call's %v — the answer is not being kept",
			rest, firstTook)
	}
}

// The ordinary case still has to work, or the timeout has been bought by
// breaking the thing it protects.
func TestPowerShellProbeReturnsOutput(t *testing.T) {
	out, err := powershellOutput(context.Background(), availabilityTimeout, "Write-Output 42")
	if err != nil {
		t.Fatalf("a trivial probe failed: %v", err)
	}
	if got := string(out); len(got) == 0 || got[0] != '4' {
		t.Errorf("probe returned %q, want it to start with 42", got)
	}
}
