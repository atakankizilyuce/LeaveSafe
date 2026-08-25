package monitor

import (
	"sync"

	"github.com/leavesafe/leavesafe/internal/safe"
)

// Whether a sensor can work on this machine is settled once and then kept here,
// because finding out is not always cheap: on Windows the lid sensor answers by
// starting PowerShell and querying WMI, under a twenty-second budget.
//
// The budget is defensible on its own terms — the question is asked once per run
// and a wrong answer quietly removes a sensor the owner is relying on. What was
// never decided is *who pays it*. The answer turned out to be whoever asked
// first, and one of the things that asks is the reply to a pairing key. So a
// phone could sit waiting on WMI, and it gives up after ten seconds.
//
// The cache lives on the Manager rather than on each sensor so the guarantee is
// structural. Put it behind an interface sensors opt into and the next sensor
// with a slow probe reintroduces the bug by forgetting to implement it.

// availabilityTask is the prefix every one of these probes is named
// under, and the name is what a panic in one is reported as. Written
// once because all three callers below have to agree on it: a probe
// named differently in one of them is a crash nobody can place.
const availabilityTask = "sensor-availability:"

// AvailableNow reports whether s can work here, without ever waiting to find
// out. known is false while the answer is still being worked out, and the first
// caller to find it missing starts the work in the background.
//
// Use it for anything a client is waiting on — the pairing reply, the status
// broadcast. A sensor that has not answered yet reads as unavailable, which is
// the safe direction for an alarm and is corrected by the next broadcast.
func (m *Manager) AvailableNow(s Sensor) (available, known bool) {
	name := s.Name()

	m.mu.Lock()
	if a, ok := m.availability[name]; ok {
		m.mu.Unlock()
		return a, true
	}
	if m.probing[name] {
		m.mu.Unlock()
		return false, false
	}
	m.probing[name] = true
	m.mu.Unlock()

	safe.Go(availabilityTask+name, func() { m.probe(s) })
	return false, false
}

// probe asks one sensor and records the answer. It must not be called with
// m.mu held: that is the whole point of it.
func (m *Manager) probe(s Sensor) {
	answer := s.Available()

	m.mu.Lock()
	m.availability[s.Name()] = answer
	delete(m.probing, s.Name())
	m.mu.Unlock()
}

// availabilityFor answers "can this sensor work here" for a whole set of
// sensors, waiting only for the ones nobody has asked yet, and asking those
// side by side.
//
// This is what arming reads. Arming is allowed to wait — it is a deliberate act
// with a person behind it, and starting the wrong set of sensors is worse than
// a pause — but it used to pay for that by asking every sensor in turn, from
// scratch, on whichever goroutine typed `arm`. Six sensors each allowed twenty
// seconds is two minutes, and on the console that is two minutes with nobody
// reading the keyboard. The answers were already settled by PrimeAvailability
// at startup; nothing was learned by asking again.
//
// So: settled answers are used, unsettled ones are asked for at once rather
// than one after another, and the worst case is one probe instead of the sum of
// them. It still waits outside the lock — see probe.
func (m *Manager) availabilityFor(sensors []Sensor) map[string]bool {
	answers := make(map[string]bool, len(sensors))

	m.mu.RLock()
	var unsettled []Sensor
	for _, s := range sensors {
		if a, ok := m.availability[s.Name()]; ok {
			answers[s.Name()] = a
			continue
		}
		unsettled = append(unsettled, s)
	}
	m.mu.RUnlock()

	if len(unsettled) == 0 {
		return answers
	}

	var wg sync.WaitGroup
	for _, s := range unsettled {
		wg.Add(1)
		sensor := s
		safe.Go(availabilityTask+sensor.Name(), func() {
			defer wg.Done()
			m.probe(sensor)
		})
	}
	wg.Wait()

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range unsettled {
		answers[s.Name()] = m.availability[s.Name()]
	}
	return answers
}

// PrimeAvailability asks every sensor whether it can work here, so the answer is
// settled before anything on a request path wants it.
//
// The probes run side by side. One after another, six sensors each allowed
// twenty seconds is two minutes of a laptop not knowing what it can watch.
//
// It blocks until every sensor has answered, so a caller that must not wait —
// startup is one — runs it on a goroutine of its own.
func (m *Manager) PrimeAvailability() {
	var wg sync.WaitGroup
	for _, s := range m.Sensors() {
		wg.Add(1)
		sensor := s
		safe.Go(availabilityTask+sensor.Name(), func() {
			defer wg.Done()
			m.probe(sensor)
		})
	}
	wg.Wait()
}
