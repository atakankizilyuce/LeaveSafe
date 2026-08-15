package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// What a sensor is handed so that a test can play the hardware.
//
// The seam is two fields — how the sensor reads, and how often — which is what
// lets the loop be driven without a laptop and without waiting two seconds for
// every reading. What stays untested is the one line that asks the operating
// system, and that line has nothing in it to get wrong.

// pollFast is short enough that a test never waits on the hardware clock and
// long enough that the loop is a loop rather than a spin.
const pollFast = time.Millisecond

// step is one look at the hardware: what it answered, or that it would not.
type step[T any] struct {
	value T
	err   error
}

func answers[T any](v T) step[T] { return step[T]{value: v} }

func refuses[T any]() step[T] {
	return step[T]{err: errors.New("the hardware would not answer")}
}

// readings hands out one answer after another and then repeats the last one,
// which is what hardware that has stopped moving looks like.
//
// Scripted in full before the sensor starts, so what a test is trying is
// exactly what the sensor sees. Reaching in while the loop is running raced it,
// and a racing test of an alarm is worse than no test of one.
type readings[T any] struct {
	mu    sync.Mutex
	steps []step[T]
	at    int
}

func scripted[T any](values ...T) *readings[T] {
	steps := make([]step[T], len(values))
	for i, v := range values {
		steps[i] = answers(v)
	}
	return &readings[T]{steps: steps}
}

func scriptedSteps[T any](steps ...step[T]) *readings[T] {
	return &readings[T]{steps: steps}
}

func (r *readings[T]) next() (T, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.steps) == 0 {
		var zero T
		return zero, errors.New("nothing scripted")
	}
	at := r.at
	if at < len(r.steps)-1 {
		r.at++
	}
	return r.steps[at].value, r.steps[at].err
}

// asked is how the lid and the screen take their readings: they are given the
// sensor's own context so that disarming interrupts a query rather than waiting
// on one that may not return.
func (r *readings[T]) asked(context.Context) (T, error) { return r.next() }

// watching runs a sensor for the length of the test and hands back what it
// says. The returned stop is what a disarm does.
func watching(t *testing.T, s Sensor) (alerts chan Alert, stop context.CancelFunc, done chan error) {
	t.Helper()

	alerts = make(chan Alert, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done = make(chan error, 1)

	// Sent and then closed, so a test that waits for the sensor to finish and
	// the cleanup that waits again both get an answer.
	go func() {
		done <- s.Start(ctx, alerts)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("the sensor did not stop when it was asked to")
		}
	})
	return alerts, cancel, done
}

// expectAlert waits for one thing to be said.
func expectAlert(t *testing.T, alerts <-chan Alert) Alert {
	t.Helper()
	select {
	case alert := <-alerts:
		return alert
	case <-time.After(3 * time.Second):
		t.Fatal("the sensor said nothing")
		return Alert{}
	}
}

// expectQuiet gives the sensor room to say something it should not.
func expectQuiet(t *testing.T, alerts <-chan Alert) {
	t.Helper()
	select {
	case alert := <-alerts:
		t.Fatalf("the sensor raised %q when nothing had happened", alert.Message)
	case <-time.After(150 * time.Millisecond):
	}
}

// expectStops waits for a sensor to finish, and says whether it thought being
// stopped was a failure.
func expectStops(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("the sensor did not stop")
		return nil
	}
}
