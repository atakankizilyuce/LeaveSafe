//go:build darwin

package monitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func prepareLidTest(s *LidSensor) {
	s.events = func(context.Context) (<-chan bool, func(), error) { return make(chan bool), func() {}, nil }
}

func fakeLidIO() *lidIO {
	return &lidIO{
		matching: func(string) uintptr { return 1 }, service: func(uint32, uintptr) uint32 { return 2 },
		createPort: func(uint32) uintptr { return 3 }, source: func(uintptr) uintptr { return 4 },
		interest: func(_ uintptr, _ uint32, _ string, _ uintptr, _ uintptr, notifier *uint32) int32 {
			*notifier = 5
			return 0
		},
		release: func(uint32) int32 { return 0 }, destroyPort: func(uintptr) {},
		currentLoop: func() uintptr { return 6 }, addSource: func(uintptr, uintptr, uintptr) {},
		removeSource: func(uintptr, uintptr, uintptr) {},
		runMode:      func(uintptr, float64, bool) int32 { time.Sleep(time.Millisecond); return 0 }, mode: 7,
	}
}

func TestLidSubscriptionRoutesEventsAndCleansUp(t *testing.T) {
	api := fakeLidIO()
	var ref uintptr
	var released []uint32
	var removed, destroyed bool
	api.interest = func(_ uintptr, _ uint32, interest string, _ uintptr, id uintptr, notifier *uint32) int32 {
		if interest != "IOGeneralInterest" {
			t.Errorf("interest=%s", interest)
		}
		ref = id
		*notifier = 5
		return 0
	}
	api.release = func(id uint32) int32 { released = append(released, id); return 0 }
	api.removeSource = func(uintptr, uintptr, uintptr) { removed = true }
	api.destroyPort = func(uintptr) { destroyed = true }
	events, stop, err := api.subscribe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	deliverLidEvent(ref, 0, 123, 1)
	deliverLidEvent(ref, 0, clamshellChanged, 1)
	if <-events {
		t.Fatal("closed lid reported as open")
	}
	deliverLidEvent(ref, 0, clamshellChanged, 0)
	if !<-events {
		t.Fatal("open lid reported as closed")
	}
	// A full queue cannot block a native callback and stall the system run loop.
	for range 20 {
		deliverLidEvent(ref, 0, clamshellChanged, 1)
	}
	stop()
	stop()
	if !removed || !destroyed || len(released) != 2 {
		t.Fatalf("cleanup: %v %v %v", removed, destroyed, released)
	}
	if _, exists := lidSubscriptions.Load(ref); exists {
		t.Fatal("subscription survived stop")
	}
	deliverLidEvent(ref, 0, clamshellChanged, 1)
}

func TestLidCallbackIgnoresInvalidReceiver(t *testing.T) {
	ref := uintptr(nextLidSubscription.Add(1))
	lidSubscriptions.Store(ref, "invalid receiver")
	defer lidSubscriptions.Delete(ref)
	deliverLidEvent(ref, 0, clamshellChanged, 1)
}

func TestLidSubscriptionRegistrationFailures(t *testing.T) {
	for _, stage := range []string{"service", "port", "source", "interest"} {
		t.Run(stage, func(t *testing.T) {
			api := fakeLidIO()
			switch stage {
			case "service":
				api.service = func(uint32, uintptr) uint32 { return 0 }
			case "port":
				api.createPort = func(uint32) uintptr { return 0 }
			case "source":
				api.source = func(uintptr) uintptr { return 0 }
			case "interest":
				api.interest = func(uintptr, uint32, string, uintptr, uintptr, *uint32) int32 { return -1 }
			}
			events, stop, err := api.subscribe(context.Background())
			if err == nil || events != nil || stop != nil {
				t.Fatalf("registration failure was hidden: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	api := fakeLidIO()
	api.service = func(uint32, uintptr) uint32 { <-ctx.Done(); return 0 }
	_, _, err := api.subscribe(ctx)
	if err == nil {
		t.Fatal("canceled registration succeeded")
	}
}

func TestLidNativeEventDoesNotWaitForPolling(t *testing.T) {
	s := NewLidSensor()
	s.every = time.Hour
	s.read = func(context.Context) (bool, error) { return true, nil }
	events := make(chan bool, 3)
	events <- false
	events <- false
	events <- true
	var stopped bool
	s.events = func(context.Context) (<-chan bool, func(), error) { return events, func() { stopped = true }, nil }
	ctx, cancel := context.WithCancel(context.Background())
	alerts := make(chan Alert, 3)
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx, alerts) }()
	for _, wanted := range []AlertLevel{AlertCritical, AlertWarning} {
		select {
		case alert := <-alerts:
			if alert.Level != wanted {
				t.Fatalf("level=%s", alert.Level)
			}
		case <-time.After(time.Second):
			t.Fatal("native event waited for a poll")
		}
	}
	cancel()
	<-done
	if !stopped || len(alerts) != 0 {
		t.Fatal("duplicate event or leaked subscription")
	}
}

func TestLidEventFailuresAreVisible(t *testing.T) {
	denied := errors.New("denied")
	s := NewLidSensor()
	s.events = func(context.Context) (<-chan bool, func(), error) { return nil, nil, denied }
	if err := s.Start(context.Background(), make(chan Alert)); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	for _, failedRead := range []bool{false, true} {
		events := make(chan bool)
		close(events)
		s.events = func(context.Context) (<-chan bool, func(), error) { return events, func() {}, nil }
		s.read = func(context.Context) (bool, error) {
			if failedRead {
				return false, denied
			}
			return true, nil
		}
		if err := s.Start(context.Background(), make(chan Alert)); err == nil {
			t.Fatal("dead watcher/read was accepted")
		}
	}
}

func TestLidCancellationUnblocksAlertDelivery(t *testing.T) {
	s := NewLidSensor()
	s.every = time.Hour
	s.read = func(context.Context) (bool, error) { return true, nil }
	events := make(chan bool, 1)
	events <- false
	s.events = func(context.Context) (<-chan bool, func(), error) { return events, func() {}, nil }
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = s.Start(ctx, make(chan Alert)) }()
	cancel()
	wg.Wait()
}

func TestLidNativeAdapterReportsUnavailableServices(t *testing.T) {
	savedIO, savedError := nativeLidIO, nativeLidError
	defer func() { nativeLidIO, nativeLidError = savedIO, savedError }()
	denied := errors.New("IOKit unavailable")
	nativeLidError = denied
	if _, _, err := subscribeLid(context.Background()); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	nativeLidIO, nativeLidError = fakeLidIO(), nil
	_, stop, err := subscribeLid(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stop()
}
