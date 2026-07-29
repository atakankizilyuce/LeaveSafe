#!/usr/bin/env bash
#
# Generate package manager manifests for a published release.
#
# The manifests carry checksums, and a checksum written by hand is a checksum
# that is wrong on the second release. This downloads what was actually
# published, hashes it, and fills the templates in — so the manifests describe
# the files users will download rather than the ones someone meant to publish.
#
# Usage: packaging/generate.sh <tag> <output-dir>
#   e.g. packaging/generate.sh v1.2.0 dist-manifests
#
# Run it after the release artifacts are uploaded. It needs curl and sha256sum
# (or shasum), nothing else.

set -euo pipefail

TAG="${1:-}"
OUT="${2:-dist-manifests}"

if [ -z "$TAG" ]; then
    echo "usage: $0 <tag> [output-dir]" >&2
    echo "  e.g. $0 v1.2.0 dist-manifests" >&2
    exit 2
fi

REPO="atakankizilyuce/LeaveSafe"
BASE="https://github.com/$REPO/releases/download/$TAG"
# Package managers want a bare version; the tag carries a leading v.
VERSION="${TAG#v}"

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$OUT"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# sha256sum on Linux, shasum on macOS. Both are present on their platform and
# neither is present on both.
sha256_of() {
    if command -v sha256sum > /dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    else
        shasum -a 256 "$1" | cut -d' ' -f1
    fi
}

# fetch_sha downloads a release asset and prints its SHA-256. The download is
# what makes the checksum trustworthy: hashing a local build would describe a
# file nobody downloads.
fetch_sha() {
    local name="$1"
    local dest="$WORK/$name"
    if ! curl -fsSL --retry 3 --retry-delay 2 -o "$dest" "$BASE/$name"; then
        echo "error: could not download $BASE/$name" >&2
        echo "       Has the release finished publishing?" >&2
        exit 1
    fi
    sha256_of "$dest"
}

# winget records when a version was published. Generating manifests on the day
# of the release makes today's date the right answer; run this later and pass
# LEAVESAFE_RELEASE_DATE to correct it.
RELEASE_DATE="${LEAVESAFE_RELEASE_DATE:-$(date -u +%Y-%m-%d)}"

echo "Fetching release assets for $TAG…" >&2
SHA_DARWIN_ARM64="$(fetch_sha leavesafe-darwin-arm64)"
SHA_DARWIN_AMD64="$(fetch_sha leavesafe-darwin-amd64)"
SHA_LINUX_AMD64="$(fetch_sha leavesafe-linux-amd64)"
SHA_LINUX_ARM64="$(fetch_sha leavesafe-linux-arm64)"
SHA_WINDOWS_AMD64="$(fetch_sha leavesafe-windows-amd64.exe)"

# render substitutes the placeholders in a template. sed rather than envsubst,
# which is not installed on macOS.
render() {
    sed \
        -e "s|@VERSION@|$VERSION|g" \
        -e "s|@TAG@|$TAG|g" \
        -e "s|@BASE@|$BASE|g" \
        -e "s|@RELEASE_DATE@|$RELEASE_DATE|g" \
        -e "s|@SHA_DARWIN_ARM64@|$SHA_DARWIN_ARM64|g" \
        -e "s|@SHA_DARWIN_AMD64@|$SHA_DARWIN_AMD64|g" \
        -e "s|@SHA_LINUX_AMD64@|$SHA_LINUX_AMD64|g" \
        -e "s|@SHA_LINUX_ARM64@|$SHA_LINUX_ARM64|g" \
        -e "s|@SHA_WINDOWS_AMD64@|$SHA_WINDOWS_AMD64|g" \
        "$1"
}

render "$HERE/templates/leavesafe.rb.in" > "$OUT/leavesafe.rb"
render "$HERE/templates/leavesafe.scoop.json.in" > "$OUT/leavesafe.json"
render "$HERE/templates/winget.installer.yaml.in" > "$OUT/LeaveSafe.LeaveSafe.installer.yaml"
render "$HERE/templates/winget.locale.yaml.in" > "$OUT/LeaveSafe.LeaveSafe.locale.en-US.yaml"
render "$HERE/templates/winget.version.yaml.in" > "$OUT/LeaveSafe.LeaveSafe.yaml"

echo >&2
echo "Wrote manifests for $TAG into $OUT/:" >&2
ls -1 "$OUT" | sed 's/^/  /' >&2
echo >&2
echo "Next steps are deliberately manual — see packaging/README.md." >&2
