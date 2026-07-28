package harness

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Matrix records which sensors were genuinely exercised and which were not, so
// a reader of the CI run learns exactly what was proven. Silence about a gap is
// indistinguishable from coverage, which is why every skip carries its reason
// all the way to the job summary.
type Matrix struct {
	mu      sync.Mutex
	results map[string]string // sensor -> "triggered" or "skipped: <reason>"
}

// NewMatrix creates an empty matrix.
func NewMatrix() *Matrix {
	return &Matrix{results: make(map[string]string)}
}

const skipPrefix = "skipped: "

// Triggered records that a real hardware change was performed and detected.
func (m *Matrix) Triggered(sensor string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[sensor] = "triggered"
}

// Skipped records that no real trigger was possible, and why. A skip never
// overwrites a sensor that was genuinely triggered.
func (m *Matrix) Skipped(sensor, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.results[sensor] == "triggered" {
		return
	}
	m.results[sensor] = skipPrefix + reason
}

// WriteSummary renders the matrix to stdout and, when running in GitHub
// Actions, appends it to the job summary.
func (m *Matrix) WriteSummary() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sensors := make([]string, 0, len(m.results))
	for name := range m.results {
		sensors = append(sensors, name)
	}
	sort.Strings(sensors)

	var b strings.Builder
	fmt.Fprintf(&b, "\n### Real-trigger coverage on %s\n\n", runtime.GOOS)
	b.WriteString("| Sensor | Result | Reason |\n|---|---|---|\n")
	for _, name := range sensors {
		result := m.results[name]
		if strings.HasPrefix(result, skipPrefix) {
			fmt.Fprintf(&b, "| %s | not proven | %s |\n",
				name, strings.TrimPrefix(result, skipPrefix))
			continue
		}
		fmt.Fprintf(&b, "| %s | real hardware change detected | — |\n", name)
	}
	b.WriteString("\nWhat no CI environment can reach is listed in `docs/manual-verification.md`.\n")

	fmt.Print(b.String())

	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return nil
	}
	// #nosec G304 -- the path is supplied by the CI runner, not by user input
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open step summary: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(b.String()); err != nil {
		return fmt.Errorf("write step summary: %w", err)
	}
	return nil
}
