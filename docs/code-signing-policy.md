# Code signing policy

This document says who can cause a LeaveSafe binary to be signed, what the
signature does and does not prove, what the program sends over the network, and
how to remove it. It exists because a signature is a claim about origin, and a
claim nobody can inspect is worth very little.

**Status.** Windows releases are not signed yet. This policy describes the
process that governs them once signing is in place, and the sections on
building, verifying, privacy and uninstallation describe what already happens
today.

## Attribution

Free code signing provided by [SignPath.io](https://signpath.io), certificate by
[SignPath Foundation](https://signpath.org).

The certificate belongs to the SignPath Foundation, so **the publisher named in
a signed binary is "SignPath Foundation" rather than this project**. That is how
the foundation's free signing works, and it is worth knowing before you inspect
a file's properties and find a name you did not expect. What ties the file to this
project is the build provenance attestation below, which names the repository,
the workflow and the commit.

## Roles

LeaveSafe has one maintainer, [@atakankizilyuce](https://github.com/atakankizilyuce),
who fills every role the signing process defines:

| Role | Who | What they do |
|---|---|---|
| Author | @atakankizilyuce | Writes the code and the build scripts |
| Reviewer | @atakankizilyuce | Reviews what goes onto `main` |
| Approver | @atakankizilyuce | Approves each signing request |

A one-person project cannot separate those duties, and pretending otherwise
would be the dishonest way to write this table. What stands in for separation is
that none of the three can act in private: every build runs in public GitHub
Actions from a public repository, every release is cut from a tag, and every
artifact carries an attestation naming the commit it was built from. A signature
requested for anything else would not match a build anyone can reproduce.

Contributors other than the maintainer are welcome, and their pull requests are
reviewed before merging. No contributor can request a signature.

## What is signed

The Windows executable published on the
[Releases page](https://github.com/atakankizilyuce/LeaveSafe/releases):

```
leavesafe-windows-amd64.exe
```

macOS builds are signed and notarized separately with an Apple Developer ID,
which Apple issues only to its own members and which no third party can provide.
Linux builds are not signed; the platform has no equivalent mechanism, and the
checksums and attestation below serve the same purpose there.

## How a release is built

Every published binary is produced by
[`.github/workflows/release.yml`](../.github/workflows/release.yml) running on
GitHub's hosted runners, from a tag on this repository. Nothing is built on a
maintainer's machine and uploaded. Each action the workflow uses is pinned to a
commit SHA rather than a tag, so the code that runs is the code that was
reviewed.

The full CI gate — formatting, spelling, linting on three operating systems,
tests on three operating systems, end-to-end runs, the frontend build check and
a vulnerability scan — passes before a single artifact is published.

## Verifying a download

A signature says a file was signed. The attestation says which workflow, at
which commit, produced it — a narrower claim, and the one worth checking:

```bash
gh attestation verify leavesafe-windows-amd64.exe --repo atakankizilyuce/LeaveSafe
```

A `.sha256` ships beside every artifact, and a CycloneDX SBOM listing every
dependency is attached to each release. On Windows the signature itself is
readable with:

```powershell
Get-AuthenticodeSignature .\leavesafe-windows-amd64.exe | Format-List Status, SignerCertificate
```

If the attestation does not verify, the file did not come from this project.
Delete it, and please [report it](https://github.com/atakankizilyuce/LeaveSafe/security/advisories/new).

## Privacy

LeaveSafe has no account system, no server of its own, no analytics and no
telemetry. Your phone talks directly to your laptop, and the pairing key never
leaves the two of them. Four requests can leave the machine, and every one of
them is either optional or switchable:

| Request | When | What it discloses |
|---|---|---|
| `api.github.com` | Once a day, to ask whether a newer release exists | Your IP, and from the `User-Agent` that this is LeaveSafe and which version. Nothing else. Off with `"update_check": false` |
| `ipapi.co` | Only with location reporting turned on, which is **off by default** | Your IP. Configurable with `ip_lookup_url` |
| Google Geolocation API | Only with location reporting on **and** an API key set | The MAC addresses and signal strengths of up to 24 nearby access points. Never your IP. Configurable with `geolocate_url` |

Everything else — the event history, the pairing key, the configuration, the
position — stays in the configuration directory on your own machine
([where that is](configuration.md)). Nothing is uploaded, and nothing is shared
with the maintainer.

## Uninstalling

LeaveSafe is one file and installs nothing behind your back, so removing it is
removing the file. If you asked it to start with your machine, take that entry
out first:

```bash
leavesafe uninstall-service   # removes the autostart entry, if you added one
```

Then delete the binary, or use the package manager you installed it with
(`brew uninstall leavesafe`, `winget uninstall LeaveSafe.LeaveSafe`,
`scoop uninstall leavesafe`).

The configuration directory outlives the binary on purpose, so that reinstalling
does not lock out a paired phone. Delete it by hand to remove the pairing key,
the event history and every setting:

| Platform | Directory |
|---|---|
| Windows | `%APPDATA%\LeaveSafe\` |
| Linux / macOS | `~/.leavesafe/` |

## Reporting a problem

A security flaw goes through the private route in [`SECURITY.md`](../SECURITY.md).
Anything else — including a signed file you cannot verify — belongs in a
[normal issue](https://github.com/atakankizilyuce/LeaveSafe/issues).
