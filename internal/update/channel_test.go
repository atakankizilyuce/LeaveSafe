package update

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		path string
		want Method
	}{
		// Homebrew, across every prefix it uses. Matching the Cellar segment
		// covers all of them without naming any.
		{"/opt/homebrew/Cellar/leavesafe/1.2.0/bin/leavesafe", MethodHomebrew},
		{"/usr/local/Cellar/leavesafe/1.2.0/bin/leavesafe", MethodHomebrew},
		{"/home/linuxbrew/.linuxbrew/Cellar/leavesafe/1.2.0/bin/leavesafe", MethodHomebrew},
		{"/opt/homebrew/cellar/leavesafe/1.2.0/bin/leavesafe", MethodHomebrew},

		// Scoop, with either separator: the same install can be reported both
		// ways depending on how the path was obtained.
		{`C:\Users\ataka\scoop\apps\leavesafe\current\leavesafe.exe`, MethodScoop},
		{"C:/Users/ataka/scoop/apps/leavesafe/1.2.0/leavesafe.exe", MethodScoop},
		{`D:\SCOOP\Apps\LeaveSafe\current\leavesafe.exe`, MethodScoop},

		// winget.
		{`C:\Users\ataka\AppData\Local\Microsoft\WinGet\Packages\LeaveSafe.LeaveSafe_x\leavesafe.exe`, MethodWinget},
		{"C:/Users/ataka/AppData/Local/Microsoft/WinGet/Packages/LeaveSafe.LeaveSafe_x/leavesafe.exe", MethodWinget},

		// Downloaded from the releases page, built from source, or anywhere else.
		{`C:\Users\ataka\Downloads\leavesafe-windows-amd64.exe`, MethodUnknown},
		{"/home/ataka/Downloads/leavesafe-linux-amd64", MethodUnknown},
		{"/usr/local/bin/leavesafe", MethodUnknown},
		{"./leavesafe", MethodUnknown},
		{"", MethodUnknown},

		// Paths that merely resemble a match must not produce a wrong command.
		{"/opt/homebrew/Cellar/otherapp/1.0.0/bin/otherapp", MethodUnknown},
		{`C:\Users\ataka\scoop\apps\somethingelse\current\app.exe`, MethodUnknown},
		{"/home/ataka/my-cellar/leavesafe", MethodUnknown},
		{"/home/ataka/scoop-notes/leavesafe", MethodUnknown},
	}

	for _, c := range cases {
		if got := Detect(c.path); got != c.want {
			t.Errorf("Detect(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestMethodCommand(t *testing.T) {
	cases := map[Method]string{
		MethodHomebrew: "brew upgrade leavesafe",
		MethodScoop:    "scoop update leavesafe",
		MethodWinget:   "winget upgrade LeaveSafe.LeaveSafe",
		MethodUnknown:  "",
	}
	for method, want := range cases {
		if got := method.Command(); got != want {
			t.Errorf("%v.Command() = %q, want %q", method, got, want)
		}
	}
}

func TestMethodString(t *testing.T) {
	cases := map[Method]string{
		MethodHomebrew: "homebrew",
		MethodScoop:    "scoop",
		MethodWinget:   "winget",
		MethodUnknown:  "",
	}
	for method, want := range cases {
		if got := method.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", method, got, want)
		}
	}
}

// Whatever this machine is, asking must not panic and must not invent a command
// for an installation it did not recognize.
func TestDetectSelfIsSafe(t *testing.T) {
	m := DetectSelf()
	if m == MethodUnknown && m.Command() != "" {
		t.Error("an unrecognized installation was given a command to run")
	}
}
