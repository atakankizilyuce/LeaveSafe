package monitor

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type fakeMotionReader struct {
	next   func() (motionReading, error)
	closed bool
}

func (r *fakeMotionReader) read() (motionReading, error) { return r.next() }
func (r *fakeMotionReader) close()                       { r.closed = true }

func testMotion(r *fakeMotionReader, now time.Time) *MotionSensor {
	return &MotionSensor{open: func() (motionReader, error) { return r, nil }, every: time.Millisecond, now: func() time.Time { return now }}
}

func TestMotionDetectorUsesFixedReferenceAndRejectsSingleSpikes(t *testing.T) {
	d := motionDetector{reference: motionReading{z: 1}}
	for i, x := range []float64{0, .05, .2, 0, .2, .2} {
		if d.moved(motionReading{x: x, z: 1}) {
			t.Fatalf("premature alarm at %d", i)
		}
	}
	if !d.moved(motionReading{x: .2, z: 1}) {
		t.Fatal("sustained movement was missed")
	}
	d.triggered = true
	if d.moved(motionReading{x: 1, z: 1}) {
		t.Fatal("repeated alarm within same arm")
	}
	d = motionDetector{reference: motionReading{z: 1}}
	if !d.moved(motionReading{z: .3}) {
		t.Fatal("large movement was missed")
	}
}

func TestMotionReadingRequiresFiniteFreshData(t *testing.T) {
	now := time.Now()
	for name, r := range map[string]motionReading{
		"missing time": {z: 1},
		"stale":        {z: 1, at: now.Add(-time.Second)},
		"future":       {z: 1, at: now.Add(2 * time.Second)},
		"nan":          {x: math.NaN(), at: now},
		"infinite":     {y: math.Inf(1), at: now},
	} {
		t.Run(name, func(t *testing.T) {
			if r.valid(now) {
				t.Fatal("invalid reading accepted")
			}
		})
	}
	if !(motionReading{z: 1, at: now}).valid(now) {
		t.Fatal("fresh stationary reading rejected")
	}
}

func TestMotionAvailabilityWarmsUpAndClosesReader(t *testing.T) {
	now := time.Now()
	calls := 0
	r := &fakeMotionReader{next: func() (motionReading, error) {
		calls++
		if calls == 1 {
			return motionReading{}, errMotionUnavailable
		}
		return motionReading{z: 1, at: now}, nil
	}}
	s := testMotion(r, now)
	if s.Name() != "motion" || s.DisplayName() != "Motion" || s.Stop() != nil {
		t.Fatal("sensor contract")
	}
	if !s.Available() || !r.closed || calls != 2 {
		t.Fatal("availability requires a reading and cleanup")
	}
	s.open = func() (motionReader, error) { return nil, errMotionUnavailable }
	if s.Available() {
		t.Fatal("missing sensor reported available")
	}
	if s.Start(context.Background(), make(chan Alert)) == nil {
		t.Fatal("missing sensor started")
	}
}

func TestMotionWarmupCancellation(t *testing.T) {
	r := &fakeMotionReader{next: func() (motionReading, error) { return motionReading{}, nil }}
	s := testMotion(r, time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.firstReading(ctx, r); err == nil {
		t.Fatal("empty reading accepted")
	}
	if !errors.Is(s.Start(ctx, make(chan Alert)), context.Canceled) {
		t.Fatal("canceled sensor opened")
	}
}

func TestMotionWatchDeduplicatesReadingsAndStopsAfterDisarm(t *testing.T) {
	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Three copies of one tilted sample are still only one measurement.
	readings := []motionReading{
		{z: 1, at: now},
		{x: .2, z: 1, at: now.Add(time.Millisecond)},
		{x: .2, z: 1, at: now.Add(time.Millisecond)},
		{x: .2, z: 1, at: now.Add(time.Millisecond)},
		{z: 1, at: now.Add(2 * time.Millisecond)},
		{x: .2, z: 1, at: now.Add(3 * time.Millisecond)},
		{x: .2, z: 1, at: now.Add(4 * time.Millisecond)},
		{x: .2, z: 1, at: now.Add(5 * time.Millisecond)},
		{x: 1, z: 1, at: now.Add(6 * time.Millisecond)},
	}
	i := 0
	alerts := make(chan Alert, 10)
	r := &fakeMotionReader{next: func() (motionReading, error) {
		if i < 7 && len(alerts) != 0 {
			t.Error("alarm before three distinct readings")
		}
		if i == len(readings) {
			cancel()
			return readings[i-1], nil
		}
		reading := readings[i]
		i++
		return reading, nil
	}}
	s := testMotion(r, now)
	if !errors.Is(s.Start(ctx, alerts), context.Canceled) {
		t.Fatal("watch failed to stop")
	}
	if !r.closed || len(alerts) != 1 {
		t.Fatalf("closed=%v alerts=%d", r.closed, len(alerts))
	}
	alert := <-alerts
	if alert.Sensor != "motion" || alert.Level != AlertCritical {
		t.Fatalf("wrong alarm: %+v", alert)
	}
}

func TestMotionFailureWithdrawsCoverage(t *testing.T) {
	now := time.Now()
	for _, fault := range []bool{false, true} {
		t.Run(map[bool]string{true: "driver", false: "stale"}[fault], func(t *testing.T) {
			calls := 0
			r := &fakeMotionReader{next: func() (motionReading, error) {
				calls++
				if calls == 1 {
					return motionReading{z: 1, at: now}, nil
				}
				if fault {
					return motionReading{}, errors.New("driver stopped")
				}
				return motionReading{z: 1, at: now.Add(-time.Second)}, nil
			}}
			alerts := make(chan Alert, 1)
			if !errors.Is(testMotion(r, now).Start(context.Background(), alerts), errMotionUnavailable) {
				t.Fatal("lost sensor did not fail")
			}
			if !r.closed || len(alerts) != 0 {
				t.Fatal("fault fabricated movement or leaked reader")
			}
		})
	}
}

func TestMotionDisarmUnblocksFullAlarmQueue(t *testing.T) {
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	calls := 0
	r := &fakeMotionReader{next: func() (motionReading, error) {
		calls++
		return motionReading{x: float64(calls), at: now.Add(time.Duration(calls) * time.Millisecond)}, nil
	}}
	if !errors.Is(testMotion(r, now).Start(ctx, make(chan Alert)), context.DeadlineExceeded) {
		t.Fatal("blocked alarm ignored disarm")
	}
	if !r.closed {
		t.Fatal("reader not released")
	}
}

func TestMotionRetryPreservesArmReferenceAndAlarmLatch(t *testing.T) {
	for _, alarmBeforeLoss := range []bool{false, true} {
		t.Run(map[bool]string{false: "movement during outage", true: "already alarmed"}[alarmBeforeLoss], func(t *testing.T) {
			now := time.Now()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			alerts := make(chan Alert, 10)
			attempt, ready := 0, 0
			s := &MotionSensor{every: time.Millisecond, now: func() time.Time { return now }}
			s.open = func() (motionReader, error) {
				attempt++
				values := []float64{1, 1, 1, 1}
				if attempt == 1 {
					values = []float64{0, 0}
					if alarmBeforeLoss {
						values[1] = 1
					}
				}
				i := 0
				return &fakeMotionReader{next: func() (motionReading, error) {
					if i == len(values) {
						return motionReading{}, errMotionUnavailable
					}
					reading := motionReading{x: values[i], z: 1, at: now.Add(time.Duration(i+attempt*10) * time.Millisecond)}
					i++
					return reading, nil
				}}, nil
			}
			for range 2 {
				if !errors.Is(s.StartWithReady(ctx, alerts, func() { ready++ }), errMotionUnavailable) {
					t.Fatal("driver outage was not reported")
				}
			}
			if ready != 2 || len(alerts) != 1 {
				t.Fatalf("retry reset the reference or latch: ready=%d alerts=%d", ready, len(alerts))
			}
			cancel()
			rearmed, disarm := context.WithCancel(context.Background())
			defer disarm()
			if !errors.Is(s.Start(rearmed, alerts), errMotionUnavailable) || len(alerts) != 1 {
				t.Fatal("new arm did not establish its own stationary reference")
			}
		})
	}
}

type warmingMotionSensor struct {
	failingSensor
	started  chan struct{}
	proceed  chan struct{}
	watching chan struct{}
}

func (s *warmingMotionSensor) StartWithReady(ctx context.Context, _ chan<- Alert, ready func()) error {
	close(s.started)
	<-s.proceed
	ready()
	close(s.watching)
	<-ctx.Done()
	return ctx.Err()
}

func TestMotionManagerWaitsForLiveReading(t *testing.T) {
	s := &warmingMotionSensor{failingSensor: failingSensor{name: "motion", available: true}, started: make(chan struct{}), proceed: make(chan struct{}), watching: make(chan struct{})}
	m := NewManager()
	m.Register(s)
	m.Enable(s.Name())
	m.StartEnabled()
	defer m.StopAll()
	<-s.started
	if m.Failure(s.Name()) == "" {
		t.Error("warm-up incorrectly reported live coverage")
	}
	close(s.proceed)
	<-s.watching
	if m.Failure(s.Name()) != "" {
		t.Error("live reading did not restore coverage")
	}
}

func TestMotionLateReadyCannotClearNewSessionFailure(t *testing.T) {
	s := &warmingMotionSensor{failingSensor: failingSensor{name: "motion", available: true}, started: make(chan struct{}), proceed: make(chan struct{}), watching: make(chan struct{})}
	m := NewManager()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = m.startSensor(ctx, s, make(chan Alert)) }()
	<-s.started
	cancel()
	m.recordFailure(s.Name(), errMotionUnavailable)
	close(s.proceed)
	<-done
	if m.Failure(s.Name()) == "" {
		t.Fatal("canceled attempt claimed coverage for a later session")
	}
}
