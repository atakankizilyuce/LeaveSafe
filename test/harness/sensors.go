package harness

// allSensors is every sensor the binary registers, so a test can say which one
// it is measuring and switch the rest off rather than leaving them to chance.
var allSensors = []string{"power", "lid", "usb", "screen", "network", "input"}

// OnlySensor is the sensor map that leaves one sensor watching.
//
// Naming all of them matters: they are all on by default, and the first alarm
// on an armed machine suppresses every alert behind it. A second sensor moving
// while the test sets up — a runner whose own lid or charger state shifted, a
// VM whose hardware settled late — would swallow the very alert the test then
// waits for, and the failure would read as "the sensor did not fire".
func OnlySensor(name string) map[string]bool {
	want := make(map[string]bool, len(allSensors))
	for _, s := range allSensors {
		want[s] = s == name
	}
	return want
}
