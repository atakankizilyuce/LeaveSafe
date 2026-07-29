# Release signing

LeaveSafe's install instructions used to include this:

```bash
xattr -d com.apple.quarantine leavesafe-darwin-arm64
```

That is a security tool teaching its users to disarm the exact warning that
would catch a tampered copy. It is there because the binary is unsigned, and the
fix is to sign it — not to explain the workaround more clearly.

The release workflow already has both signing steps. They **skip themselves when
their secrets are absent**, so a fork, or this repository before a certificate
exists, still produces a working release; the run summary says the artifact went
out unsigned. This page is what to set to turn them on.

---

## macOS: Developer ID + notarization

Two things have to be true before macOS runs a downloaded binary without a
fight: it is signed with a **Developer ID Application** certificate, and Apple
has **notarized** it.

### What you need

- An [Apple Developer Program](https://developer.apple.com/programs/) membership
  (99 USD/year). There is no free path — Apple issues Developer ID certificates
  only to paid members.
- A **Developer ID Application** certificate, created in the developer portal
  and exported from Keychain Access as a `.p12` with a password.
- An **app-specific password** for notarization, from
  [appleid.apple.com](https://appleid.apple.com) → Sign-In and Security →
  App-Specific Passwords. Not your Apple ID password.

### Repository secrets

| Secret | Value |
|--------|-------|
| `MACOS_CERTIFICATE` | The `.p12`, base64-encoded: `base64 -i cert.p12 \| pbcopy` |
| `MACOS_CERTIFICATE_PASSWORD` | The password you set when exporting it |
| `MACOS_SIGN_IDENTITY` | The identity name, e.g. `Developer ID Application: Your Name (TEAMID)` — list them with `security find-identity -v -p codesigning` |
| `MACOS_NOTARY_APPLE_ID` | The Apple ID email the membership belongs to |
| `MACOS_NOTARY_TEAM_ID` | Your ten-character Team ID, in the portal's Membership section |
| `MACOS_NOTARY_PASSWORD` | The app-specific password |

### What the workflow does

Imports the certificate into a throwaway keychain that exists only for that job,
signs with `--options runtime --timestamp`, submits to `notarytool` and waits for
the verdict, then runs `spctl --assess` so a rejected notarization fails the
release rather than the user's first launch.

A lone executable cannot carry a stapled ticket the way an app bundle can, so
Gatekeeper checks the notarization online the first time it runs. That is fine —
it just means the first launch on a brand-new machine wants a network.

---

## Windows: Authenticode

Without a signature, SmartScreen shows "Windows protected your PC" and hides the
Run button behind **More info**. Most people stop there.

### What you need

A code signing certificate from a CA (DigiCert, Sectigo, SSL.com and others).
Two kinds exist, and the difference matters:

- **OV (Organization Validation)**, roughly 200–400 USD/year. Signs fine, but
  SmartScreen reputation is built per-certificate over time and downloads —
  early users may still see the warning.
- **EV (Extended Validation)**, roughly 300–600 USD/year. Carries SmartScreen
  reputation from the first signature. Since June 2023 the private key must live
  on a hardware token or an HSM, which means an EV certificate cannot be handed
  to a GitHub-hosted runner as a `.p12`. Signing with EV needs either a
  cloud-HSM signing service (Azure Trusted Signing, DigiCert KeyLocker,
  SSL.com eSigner) or a self-hosted runner with the token attached.

The workflow as written expects a `.p12`/`.pfx`, so it fits an OV certificate.
For EV, replace the signing step with your provider's action; everything around
it — the timestamp, the checksum ordering, the attestation — stays.

### Repository secrets

| Secret | Value |
|--------|-------|
| `WINDOWS_CERTIFICATE` | The `.pfx`, base64-encoded: `base64 -w0 cert.pfx` |
| `WINDOWS_CERTIFICATE_PASSWORD` | Its password |

### What the workflow does

Signs with `osslsigncode`, SHA-256, and an RFC 3161 timestamp from DigiCert. The
timestamp is not optional: without it, every binary ever published goes untrusted
on the day the certificate expires. With it, signatures stay valid for the life
of the timestamp.

---

## What you get without paying anything

Signing costs money every year, and the project may not want that bill yet.
Three things still hold in the meantime, and they are worth telling users about:

**Build provenance.** Every release artifact carries a signed attestation naming
the workflow and commit that produced it. Anyone can check:

```bash
gh attestation verify leavesafe-linux-amd64 --repo atakankizilyuce/LeaveSafe
```

That is a stronger claim than a code signature: a signature says "someone with
this certificate signed this", while provenance says "this exact file came out of
this exact workflow run, at this commit". It does not stop the operating system
warning, because the operating system has no idea what GitHub attestations are.

**Checksums.** Every artifact ships with a `.sha256` beside it. Worth publishing,
but be honest about what it proves: it proves the file matches a number published
by whoever published the file.

**An SBOM.** A CycloneDX inventory of every dependency, attached to the release,
so a user can check what is in the binary against a vulnerability database
without building it themselves.

---

## Verifying a release by hand

```bash
# Provenance: which workflow, at which commit, built this file
gh attestation verify leavesafe-linux-amd64 --repo atakankizilyuce/LeaveSafe

# Checksum
shasum -a 256 -c leavesafe-linux-amd64.sha256

# macOS signature, once signing is on
codesign --verify --strict --verbose=2 leavesafe-darwin-arm64
spctl --assess --type execute --verbose=4 leavesafe-darwin-arm64

# Windows signature, once signing is on
signtool verify /pa /v leavesafe-windows-amd64.exe
```
