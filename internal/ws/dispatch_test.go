package ws

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/monitor"
)

// What one alert from a sensor means depends on three questions asked in order,
// and each of them can end it before the siren ever sounds: the screen locking
// arms the machine rather than alarming it, an unarmed machine ignores its
// sensors entirely, and an alarm already sounding swallows what arrives behind
// it. Getting the order wrong is not a tidiness problem — two of the three are
// what stop the alarm re-firing on itself.

func alertFrom(sensor, message string) monitor.Alert {
	return monitor.Alert{Sensor: sensor, Level: monitor.AlertCritical, Message: message}
}

// alarmState reads what the hub thinks is sounding, under the lock that guards
// it.
func alarmState(hub *Hub) (sensor string, active bool) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return hub.alarmSensor, hub.alarmActive
}

func TestScreenLockingArmsTheMachineRatherThanAlarming(t *testing.T) {
	hub, _ := hubWithSensors(t, "screen")
	listeningClient(t, hub)
	hub.SetAutoArmOnLock(true)

	hub.dispatchAlert(alertFrom("screen", "display turned off"))

	if !hub.IsArmed() {
		t.Error("the screen locking did not arm the machine")
	}
	if _, active := alarmState(hub); active {
		t.Error("the screen locking raised an alarm instead of arming")
	}
}

func TestScreenUnlockingDisarms(t *testing.T) {
	hub, _ := hubWithSensors(t, "screen")
	listeningClient(t, hub)
	hub.SetAutoArmOnLock(true)
	hub.Arm()

	hub.dispatchAlert(alertFrom("screen", "display turned on"))

	if hub.IsArmed() {
		t.Error("the screen unlocking did not disarm the machine")
	}
}

// Arming a laptop that nothing can disarm it from leaves the owner with a
// machine that will scream at them and no way to answer it — so with no phone
// paired, the screen locking is just another alert.
func TestTheScreenDoesNotAutoArmWithNoPhonePaired(t *testing.T) {
	hub, _ := hubWithSensors(t, "screen")
	hub.SetAutoArmOnLock(true)

	hub.dispatchAlert(alertFrom("screen", "display turned off"))

	if hub.IsArmed() {
		t.Error("the machine armed itself with nothing paired to disarm it")
	}
}

func TestAnUnarmedMachineIgnoresItsSensors(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	listeningClient(t, hub)

	hub.dispatchAlert(alertFrom("power", "charger disconnected"))

	if _, active := alarmState(hub); active {
		t.Error("an unarmed machine raised an alarm")
	}
}

func TestAnArmedMachineRaisesTheAlarmOnTheSensorThatFired(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")
	rec := listeningClient(t, hub)
	hub.Arm()

	hub.dispatchAlert(alertFrom("power", "charger disconnected"))

	sensor, active := alarmState(hub)
	if !active || sensor != "power" {
		t.Errorf("the alarm did not name the sensor that fired; got %q active=%v", sensor, active)
	}
	if !rec.sawWarning("charger disconnected") {
		t.Errorf("the alert never reached the phone; alerts were %q", rec.warnings())
	}
}

// Every alert re-fires the siren and the trigger callback, so a sensor that
// keeps reporting would drive the alarm in a loop. The second one has to be
// dropped on the floor.
func TestASecondAlertDoesNotRestartAnAlarmAlreadySounding(t *testing.T) {
	hub, _ := hubWithSensors(t, "power", "lid")
	rec := listeningClient(t, hub)
	hub.Arm()

	hub.dispatchAlert(alertFrom("power", "charger disconnected"))
	hub.dispatchAlert(alertFrom("lid", "lid closed"))

	sensor, _ := alarmState(hub)
	if sensor != "power" {
		t.Errorf("the second alert took the alarm over; it now names %q", sensor)
	}
	if rec.sawWarning("lid closed") {
		t.Errorf("the second alert was announced anyway; alerts were %q", rec.warnings())
	}
}

// Dismissing an alarm buys a few seconds of quiet from the sensor that raised
// it. Without that, picking the laptop up to dismiss the alarm is itself the
// input that raises the next one.
func TestASuppressedSensorIsIgnoredUntilItsGracePeriodIsUp(t *testing.T) {
	hub, _ := hubWithSensors(t, "input")
	listeningClient(t, hub)
	hub.Arm()

	hub.mu.Lock()
	hub.suppressedSensors["input"] = time.Now().Add(time.Minute)
	hub.mu.Unlock()

	hub.dispatchAlert(alertFrom("input", "the laptop was moved"))

	if _, active := alarmState(hub); active {
		t.Error("a suppressed sensor raised an alarm inside its grace period")
	}
}

func TestASensorAlarmsAgainOnceItsGracePeriodHasPassed(t *testing.T) {
	hub, _ := hubWithSensors(t, "input")
	listeningClient(t, hub)
	hub.Arm()

	hub.mu.Lock()
	hub.suppressedSensors["input"] = time.Now().Add(-time.Second)
	hub.mu.Unlock()

	hub.dispatchAlert(alertFrom("input", "the laptop was moved"))

	if _, active := alarmState(hub); !active {
		t.Error("a sensor stayed suppressed after its grace period had passed")
	}
	hub.mu.RLock()
	_, stillSuppressed := hub.suppressedSensors["input"]
	hub.mu.RUnlock()
	if stillSuppressed {
		t.Error("the expired suppression was left behind to be checked forever")
	}
}
