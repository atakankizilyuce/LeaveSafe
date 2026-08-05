package main

import (
	"testing"

	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/monitor"
)

// A fresh installation has no sensor map in its config, and that used to mean
// no sensor was ever switched on. Arming such a copy started nothing: the
// dashboard read "0 / 6 active", the phone read "0 sensors ready", and the
// laptop sat there guarding itself against nothing while the user walked away.
func TestFreshConfigWatchesEverySensor(t *testing.T) {
	mgr := monitor.NewManager()
	registerSensors(mgr, config.Default())

	sensors := mgr.Sensors()
	if len(sensors) == 0 {
		t.Fatal("no sensors were registered at all")
	}
	for _, s := range sensors {
		if !mgr.IsEnabled(s.Name()) {
			t.Errorf("sensor %q is off on a fresh install, so arming would not watch it", s.Name())
		}
	}
}

// The other half of defaulting to on: a sensor the user switched off has to
// stay off across a restart, or the setting is one the program quietly undoes
// every morning.
func TestASensorTurnedOffStaysOff(t *testing.T) {
	cfg := config.Default()
	cfg.EnabledSensors = map[string]bool{"input": false}

	mgr := monitor.NewManager()
	registerSensors(mgr, cfg)

	if mgr.IsEnabled("input") {
		t.Error("input was switched back on despite the config recording it off")
	}
	for _, s := range mgr.Sensors() {
		if s.Name() == "input" {
			continue
		}
		if !mgr.IsEnabled(s.Name()) {
			t.Errorf("sensor %q was switched off, and nothing asked for that", s.Name())
		}
	}
}

// A config that names only the sensors it wants on is still honoured for those
// it names true, and says nothing about the rest.
func TestAnExplicitlyEnabledSensorStaysOn(t *testing.T) {
	cfg := config.Default()
	cfg.EnabledSensors = map[string]bool{"power": true}

	mgr := monitor.NewManager()
	registerSensors(mgr, cfg)

	if !mgr.IsEnabled("power") {
		t.Error("power is off despite the config recording it on")
	}
}

// A name nobody registered — a hand-edited config, or a sensor dropped in a
// later version — must not disturb the ones that exist.
func TestAnUnknownSensorNameChangesNothing(t *testing.T) {
	cfg := config.Default()
	cfg.EnabledSensors = map[string]bool{"barometer": false}

	mgr := monitor.NewManager()
	registerSensors(mgr, cfg)

	for _, s := range mgr.Sensors() {
		if !mgr.IsEnabled(s.Name()) {
			t.Errorf("sensor %q was switched off by an entry naming something else", s.Name())
		}
	}
}
