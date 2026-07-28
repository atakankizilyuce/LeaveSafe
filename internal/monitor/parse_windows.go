//go:build windows

package monitor

import "strings"

// The functions here interpret what PowerShell and WMI report. They are
// separated from the code that runs those queries so a format change — the kind
// that would silently turn the alarm into a no-op — is caught by a test against
// output Windows really produced. See testdata/PROVENANCE.md.
//
// Every one of these tolerates the CRLF line endings PowerShell writes.

// parseLidStatusWMI reads MSAcpi_LidStatus.LidStatus, which PowerShell renders
// as True/False on some systems and 1/0 on others.
func parseLidStatusWMI(raw string) bool {
	status := strings.TrimSpace(strings.ToLower(raw))
	return status == "true" || status == "1"
}

// parseHasBattery reads (Get-WmiObject Win32_Battery).Count. Anything other
// than zero or empty means this machine has a battery, so it is a laptop.
func parseHasBattery(raw string) bool {
	count := strings.TrimSpace(raw)
	return count != "0" && count != ""
}

// parseUSBEventLine splits one line emitted by the WMI event helper, which
// writes "<sourceIdentifier>|<device name>". Only the first separator counts,
// because device names may themselves contain a pipe.
func parseUSBEventLine(line string) (eventType, name string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// parseLogonUIPresent reports whether the LogonUI process is running, which
// means the session is locked.
func parseLogonUIPresent(raw string) bool {
	return strings.Contains(strings.TrimSpace(raw), "True")
}
