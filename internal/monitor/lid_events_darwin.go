//go:build darwin

package monitor

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

// Public IOPM.h: a clamshell notification precedes any sleep caused by that
// change. Unlike the polling fallback, it does not wait for the next tick.
const clamshellChanged = uint32(0xe0000000 | (13 << 14) | 0x100)

type lidIO struct {
	matching     func(string) uintptr
	service      func(uint32, uintptr) uint32
	createPort   func(uint32) uintptr
	source       func(uintptr) uintptr
	interest     func(uintptr, uint32, string, uintptr, uintptr, *uint32) int32
	release      func(uint32) int32
	destroyPort  func(uintptr)
	currentLoop  func() uintptr
	addSource    func(uintptr, uintptr, uintptr)
	removeSource func(uintptr, uintptr, uintptr)
	runMode      func(uintptr, float64, bool) int32
	createString func(uintptr, string, uint32) uintptr
	mode         uintptr
}

var nativeLidIO, nativeLidError = loadLidIO()
var lidSubscriptions sync.Map
var nextLidSubscription atomic.Uint64
var lidCallback = purego.NewCallback(deliverLidEvent)

func deliverLidEvent(ref uintptr, _ uint32, message uint32, bits uintptr) {
	if message != clamshellChanged {
		return
	}
	if receiver, ok := lidSubscriptions.Load(ref); ok {
		events, valid := receiver.(chan bool)
		if !valid {
			return
		}
		select {
		case events <- bits&1 == 0: // bit 0 means closed
		default: // The polling fallback also refreshes the state.
		}
	}
}

func loadLidIO() (*lidIO, error) {
	io, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_LAZY)
	if err != nil {
		return nil, err
	}
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY)
	if err != nil {
		return nil, err
	}
	api := &lidIO{}
	bindings := []struct {
		library uintptr
		name    string
		target  any
	}{
		{io, "IOServiceMatching", &api.matching}, {io, "IOServiceGetMatchingService", &api.service},
		{io, "IONotificationPortCreate", &api.createPort}, {io, "IONotificationPortGetRunLoopSource", &api.source},
		{io, "IOServiceAddInterestNotification", &api.interest}, {io, "IOObjectRelease", &api.release},
		{io, "IONotificationPortDestroy", &api.destroyPort}, {cf, "CFRunLoopGetCurrent", &api.currentLoop},
		{cf, "CFRunLoopAddSource", &api.addSource}, {cf, "CFRunLoopRemoveSource", &api.removeSource},
		{cf, "CFRunLoopRunInMode", &api.runMode}, {cf, "CFStringCreateWithCString", &api.createString},
	}
	for _, binding := range bindings {
		address, err := purego.Dlsym(binding.library, binding.name)
		if err != nil {
			return nil, err
		}
		purego.RegisterFunc(binding.target, address)
	}
	// Process-lifetime mode shared by every subscription; no pointer to a Go
	// object is retained by CoreFoundation. UTF-8 is kCFStringEncodingUTF8.
	api.mode = api.createString(0, "kCFRunLoopDefaultMode", 0x08000100)
	if api.mode == 0 {
		return nil, fmt.Errorf("cannot create run loop mode")
	}
	return api, nil
}

// Own the native port and run loop on one OS thread. Stop waits for its
// cleanup, so disarming/rearming never leaks subscriptions or native handles.
func subscribeLid(ctx context.Context) (<-chan bool, func(), error) {
	if nativeLidError != nil {
		return nil, nil, nativeLidError
	}
	return nativeLidIO.subscribe(ctx)
}

func (api *lidIO) subscribe(ctx context.Context) (<-chan bool, func(), error) {
	events := make(chan bool, 8)
	ready := make(chan error, 1)
	finished := make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)
	ref := uintptr(nextLidSubscription.Add(1))
	go func() {
		defer close(finished)
		defer close(events)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := api.observe(runCtx, ref, events, ready); err != nil {
			ready <- err
		}
	}()
	stop := func() { cancel(); <-finished }
	select {
	case err := <-ready:
		if err != nil {
			stop()
			return nil, nil, err
		}
	case <-ctx.Done():
		stop()
		return nil, nil, ctx.Err()
	}
	return events, stop, nil
}

func (api *lidIO) observe(ctx context.Context, ref uintptr, events chan bool, ready chan<- error) error {
	service := api.service(0, api.matching("IOPMrootDomain"))
	if service == 0 {
		return fmt.Errorf("IOPMrootDomain unavailable")
	}
	defer api.release(service)
	port := api.createPort(0)
	if port == 0 {
		return fmt.Errorf("cannot create lid notification port")
	}
	defer api.destroyPort(port)
	source := api.source(port)
	if source == 0 {
		return fmt.Errorf("cannot create lid notification source")
	}
	var notifier uint32
	lidSubscriptions.Store(ref, events)
	defer lidSubscriptions.Delete(ref)
	if status := api.interest(port, service, "IOGeneralInterest", lidCallback, ref, &notifier); status != 0 {
		return fmt.Errorf("register lid notifications: %d", status)
	}
	defer api.release(notifier)
	loop := api.currentLoop()
	api.addSource(loop, source, api.mode)
	defer api.removeSource(loop, source, api.mode)
	ready <- nil
	for ctx.Err() == nil {
		api.runMode(api.mode, 0.05, true)
	}
	return nil
}
