package network

import "fmt"

// FirewallCommand is what the user would run to let this port in through the
// host firewall, written for the platform they are on.
//
// It is a string and nothing more. This program does not run it, and the
// distinction is deliberate: opening a hole in a firewall is a change to the
// machine's security posture that outlives the process making it, needs
// privileges LeaveSafe does not otherwise ask for, and — on a machine somebody
// else administers — may be against a rule this program cannot know about. So
// the user is told exactly what to run and decides.
//
// It is only ever shown alongside a reachability check that did not complete,
// because the host firewall is the likeliest cause of that and the only one of
// the likely causes that is on this machine rather than out on the network.
func FirewallCommand(goos string, port int) string {
	switch goos {
	case "windows":
		// Needs an elevated prompt. Defender's default for an inbound
		// connection to a program with no rule is to block it silently when
		// there is nobody at the screen to answer the pop-up — which is exactly
		// the case LeaveSafe runs in.
		return fmt.Sprintf(`run this in an Administrator PowerShell: `+
			`netsh advfirewall firewall add rule name="LeaveSafe" dir=in action=allow `+
			`protocol=TCP localport=%d`, port)

	case "linux":
		// Two firewalls, because the distributions split roughly evenly between
		// them and naming only one would be right half the time.
		return fmt.Sprintf("run `sudo ufw allow %d/tcp`, or on a firewalld system "+
			"`sudo firewall-cmd --permanent --add-port=%d/tcp && sudo firewall-cmd --reload`",
			port, port)

	case "darwin":
		// macOS filters by application rather than by port, so the rule names
		// the binary. The usual prompt on first listen is the same decision,
		// and this is how to make it without one.
		return "allow incoming connections for LeaveSafe in System Settings → Network → Firewall → " +
			"Options, or run `sudo /usr/libexec/ApplicationFirewall/socketfilterfw " +
			"--unblockapp $(command -v leavesafe)`"

	default:
		return fmt.Sprintf("allow inbound TCP port %d in this machine's firewall", port)
	}
}
