// Package syspath resolves the operating system's own executables to absolute
// paths, so this program never asks PATH where they are.
//
// Running `powershell`, `netsh` or `schtasks` by bare name means Windows —
// and Go's exec.LookPath before it — searches every directory in PATH in order.
// Plenty of machines carry a PATH entry some installer added that ordinary
// users can write to, and it need only sit ahead of System32. Whatever is
// dropped there under the right name is then run by LeaveSafe: every couple of
// seconds while armed, in the owner's own session, for as long as the monitor
// is watching. `schtasks` raises it further, because install-service may be run
// from an elevated prompt.
//
// Nothing about the arguments was ever the problem — they are compile-time
// constants and paths this program derived. It was the question of which binary
// answers to the name, and the answer has to come from the system directory
// rather than from whatever PATH happens to say today.
//
// The non-Windows build returns names unchanged. The equivalent PATH entries
// there are root-owned, and a hijack inside a user's own ~/bin is code that
// user could already run.
package syspath
