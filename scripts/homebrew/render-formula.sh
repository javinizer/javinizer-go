#!/usr/bin/env bash
# render-formula.sh — render the Homebrew Formula/javinizer.rb for a release.
#
# Usage: render-formula.sh <tag> <checksums.txt> [template]
#   tag           release tag, e.g. v1.0.0
#   checksums.txt path to the release checksums.txt (sha256sum output)
#   template      path to the .rb.tmpl (default: scripts/homebrew/javinizer.rb.tmpl)
#
# Writes the rendered formula to stdout. Exits non-zero if any required asset
# checksum is missing, so CI never publishes a half-rendered formula.

set -euo pipefail

tag="${1:?usage: render-formula.sh <tag> <checksums.txt> [template]}"
checksums="${2:?usage: render-formula.sh <tag> <checksums.txt> [template]}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
template="${3:-${script_dir}/javinizer.rb.tmpl}"

version="${tag#v}" # Homebrew versions omit the leading 'v'

sha_for() {
    # sha256sum output is "<hash>  <name>" (or "<hash> *<name>"); field 1 is the hash.
    grep -m1 "$1" "$checksums" | awk '{print $1}'
}

darwin_sha="$(sha_for 'javinizer-darwin-universal')"
linux_amd64_sha="$(sha_for 'javinizer-linux-amd64')"
linux_arm64_sha="$(sha_for 'javinizer-linux-arm64')"

if [[ -z "$darwin_sha" || -z "$linux_amd64_sha" || -z "$linux_arm64_sha" ]]; then
    echo "error: missing one or more required checksums in $checksums" >&2
    echo "  darwin-universal: ${darwin_sha:-<missing>}" >&2
    echo "  linux-amd64:      ${linux_amd64_sha:-<missing>}" >&2
    echo "  linux-arm64:      ${linux_arm64_sha:-<missing>}" >&2
    exit 1
fi

# Use '|' as the sed delimiter so the release URLs (which contain '/') substitute cleanly.
sed \
    -e "s|__VERSION__|${version}|g" \
    -e "s|__TAG__|${tag}|g" \
    -e "s|__DARWIN_SHA256__|${darwin_sha}|g" \
    -e "s|__LINUX_AMD64_SHA256__|${linux_amd64_sha}|g" \
    -e "s|__LINUX_ARM64_SHA256__|${linux_arm64_sha}|g" \
    "$template"
