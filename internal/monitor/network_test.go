package monitor

import (
	"context"
	"testing"
)

func TestNetworkSensor_Name(t *testing.T) {
	s := NewNetworkSensor()
	if s.Name() != "network" {
		t.Errorf("Name() = %q, want %q", s.Name(), "network")
	}
}

func TestNetworkSensor_DisplayName(t *testing.T) {
	s := NewNetworkSensor()
	if s.DisplayName() != "IP Address Change" {
		t.Errorf("DisplayName() = %q, want %q", s.DisplayName(), "IP Address Change")
	}
}

func TestNetworkSensor_Available(t *testing.T) {
	s := NewNetworkSensor()
	if !s.Available() {
		t.Error("Available() = false, want true")
	}
}

func TestNetworkSensor_Stop(t *testing.T) {
	s := NewNetworkSensor()
	if err := s.Stop(); err != nil {
		t.Errorf("Stop() returned error: %v", err)
	}
}

// TestNetworkSensor_Start_CancelledContext verifies that Start returns nil
// immediately when given an already-cancelled context.
func TestNetworkSensor_Start_CancelledContext(t *testing.T) {
	s := NewNetworkSensor()
	alerts := make(chan Alert, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Start

	err := s.Start(ctx, alerts)
	if err != nil {
		t.Errorf("Start() with cancelled context returned error: %v", err)
	}
}

// TestNetworkSnapshot verifies networkSnapshot returns a non-error string.
func TestNetworkSnapshot(t *testing.T) {
	snap := networkSnapshot()
	if snap == "error" {
		t.Error("networkSnapshot() returned 'error', expected valid IP list or empty string")
	}
}
