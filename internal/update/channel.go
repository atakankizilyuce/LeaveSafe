package update

import (
	"os"
	"path/filepath"
	"strings"
)

// Method is how this copy of LeaveSafe was installed.
//
// Knowing it turns "a new version exists" into "run this to get it". The three
// package managers ship a byte-identical binary — the manifests are generated
// from the published release assets — so this cannot be stamped in at link time
// and has to be read back from where the file ended up.
type Method int

const (
	// MethodUnknown covers a file downloaded from the releases page, a build
	// from source, and anything this cannot recognize. It is the honest answer
	// far more often than it is a failure.
	MethodUnknown Method = iota
	MethodHomebrew
	MethodScoop
	MethodWinget
)

// String names the method for logs and for the phone.
func (m Method) String() string {
	switch m {
	case MethodHomebrew:
		return "homebrew"
	case MethodScoop:
		return "scoop"
	case MethodWinget:
		return "winget"
	default:
		return ""
	}
}

// Command returns the command that upgrades an installation made this way, or
// the empty string when there is no such command to offer.
//
// These are fixed strings. Nothing here is ever derived from the network: a
// command read out of release notes would let whoever writes those notes hand a
// command to every user.
func (m Method) Command() string {
	switch m {
	case MethodHomebrew:
		return "brew upgrade leavesafe"
	case MethodScoop:
		return "scoop update leavesafe"
	case MethodWinget:
		return "winget upgrade LeaveSafe.LeaveSafe"
	default:
		return ""
	}
}

// pathMarkers maps a path fragment to the method it implies.
//
// Matching Homebrew's "Cellar" segment covers all of its prefixes at once —
// /opt/homebrew, /usr/local and Linuxbrew's /home/linuxbrew/.linuxbrew — without
// hardcoding any of them.
var pathMarkers = []struct {
	fragment string
	method   Method
}{
	{"/cellar/leavesafe/", MethodHomebrew},
	{"/scoop/apps/leavesafe/", MethodScoop},
	{"/microsoft/winget/packages/", MethodWinget},
}

// Detect reports the install method implied by an executable's path.
//
// This inspects the path and nothing else. A security tool has no business
// running a subprocess to find out how it was installed, and a pure function is
// one a table test can cover on every platform at once.
func Detect(exePath string) Method {
	if exePath == "" {
		return MethodUnknown
	}
	// Compare on one separator in one case: the same installation can be
	// reported with either slash depending on how the path was obtained.
	p := strings.ToLower(filepath.ToSlash(exePath))
	for _, m := range pathMarkers {
		if strings.Contains(p, m.fragment) {
			return m.method
		}
	}
	return MethodUnknown
}

// DetectSelf reports how the running binary was installed.
//
// Homebrew links its binaries into bin/ and keeps the real file under Cellar/,
// so the symlink is resolved first. A failure at either step is not worth
// reporting: it means the releases page is offered instead of a command, which
// is right rather than wrong.
func DetectSelf() Method {
	exe, err := os.Executable()
	if err != nil {
		return MethodUnknown
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return Detect(exe)
}
