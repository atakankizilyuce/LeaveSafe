package monitor

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

// Acceleration is expressed in g, including gravity. It is never a location.
type motionReading struct {
	x, y, z float64
	at      time.Time
}

func (r motionReading) valid(now time.Time) bool {
	for _, value := range []float64{r.x, r.y, r.z} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	age := now.Sub(r.at)
	return !r.at.IsZero() && age >= -time.Second && age < time.Second
}

type motionReader interface {
	read() (motionReading, error)
	close()
}

var errMotionUnavailable = errors.New("no live accelerometer readings available")

// MotionSensor watches movement against a fixed arm-time reference. Each
// Start owns its reader; the arm context retains its baseline across retries.
// The OS adapter uses only the documented Windows Runtime sensor interface.
type MotionSensor struct {
	open  func() (motionReader, error)
	every time.Duration
	now   func() time.Time

	mu          sync.Mutex
	sessionDone <-chan struct{}
	detector    *motionDetector
}

func NewMotionSensor() *MotionSensor {
	return &MotionSensor{open: openMotionReader, every: 50 * time.Millisecond, now: time.Now}
}

func (s *MotionSensor) Name() string        { return "motion" }
func (s *MotionSensor) DisplayName() string { return "Motion" }
func (s *MotionSensor) Stop() error         { return nil }

func (s *MotionSensor) Available() bool {
	r, err := s.open()
	if err != nil {
		return false
	}
	defer r.close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = s.firstReading(ctx, r)
	return err == nil
}

// A driver can need a short warm-up after reporting is enabled. Never count
// the mere presence of a sensor object as evidence that it can watch.
func (s *MotionSensor) firstReading(ctx context.Context, r motionReader) (motionReading, error) {
	timer := time.NewTicker(s.every)
	defer timer.Stop()
	for {
		reading, err := r.read()
		if err == nil && reading.valid(s.now()) {
			return reading, nil
		}
		select {
		case <-ctx.Done():
			return motionReading{}, errMotionUnavailable
		case <-timer.C:
		}
	}
}

func (s *MotionSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	return s.StartWithReady(ctx, alerts, func() {})
}

// StartWithReady reports coverage only after a fresh sample is available.
func (s *MotionSensor) StartWithReady(ctx context.Context, alerts chan<- Alert, ready func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := s.open()
	if err != nil {
		return err
	}
	defer r.close()
	warmup, cancel := context.WithTimeout(ctx, 2*time.Second)
	reference, err := s.firstReading(warmup, r)
	cancel()
	if err != nil {
		return err
	}
	detector, err := s.session(ctx, reference)
	if err != nil {
		return err
	}
	ready()
	return s.watch(ctx, alerts, r, detector, reference.at)
}

// The supervisor reuses its context on retry and creates a new one on rearm.
// Keep the original baseline and alarm latch through a temporary driver loss.
func (s *MotionSensor) session(ctx context.Context, reference motionReading) (*motionDetector, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.detector == nil || ctx.Done() == nil || s.sessionDone != ctx.Done() {
		s.sessionDone = ctx.Done()
		s.detector = &motionDetector{reference: reference}
	}
	s.detector.consecutive = 0
	return s.detector, nil
}

func (s *MotionSensor) watch(ctx context.Context, alerts chan<- Alert, r motionReader, detector *motionDetector, last time.Time) error {
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			reading, err := r.read()
			if err != nil || !reading.valid(s.now()) {
				return errMotionUnavailable
			}
			// Polling must not turn one reading into three consecutive events.
			if !reading.at.After(last) {
				continue
			}
			last = reading.at
			if detector.moved(reading) {
				if !sendAlert(ctx, alerts, Alert{Sensor: "motion", Level: AlertCritical, Message: "Device moved!"}) {
					return ctx.Err()
				}
				// One event per arm session. Continue checking that the sensor
				// is alive; an alarm is not proof of subsequent sensor health.
				detector.triggered = true
			}
		}
	}
}

type motionDetector struct {
	reference   motionReading
	consecutive int
	triggered   bool
}

func (d *motionDetector) moved(r motionReading) bool {
	if d.triggered {
		return false
	}
	dx, dy, dz := r.x-d.reference.x, r.y-d.reference.y, r.z-d.reference.z
	distance := dx*dx + dy*dy + dz*dz
	if distance >= 0.18*0.18 {
		d.consecutive++
	} else {
		d.consecutive = 0
	}
	return distance >= 0.65*0.65 || d.consecutive >= 3
}
