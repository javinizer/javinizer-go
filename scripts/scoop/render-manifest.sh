#!/usr/bin/env bash
# render-manifest.sh — render the Scoop manifest (javinizer.json) for a release.
#
# Usage: render-manifest.sh <tag> <checksums.txt> [template]
#   tag           release tag, e.g. v1.0.0
#   checksums.txt path to the release checksums.txt (sha256sum output)
#   template      path to the .json.tmpl (default: scripts/scoop/javinizer.json.tmpl)
#
# Writes the rendered manifest to stdout. Exits non-zero if the windows asset
# checksum is missing, so CI never publishes a half-rendered manifest.

set -euo pipefail

tag="${1:?usage: render-manifest.sh <tag> <checksums.txt> [template]}"
checksums="${2:?usage: render-manifest.sh <tag> <checksums.txt> [template]}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
template="${3:-${script_dir}/javinizer.json.tmpl}"

version="${tag#v}" # Scoop versions omit the leading 'v'

sha_for() {
    # sha256sum output is "<hash>  <name>" (or "<hash> *<name>"); field 1 is the hash.
    grep -m1 "$1" "$checksums" | awk '{print $1}'
}

hash="$(sha_for 'javinizer-windows-amd64.exe')"
if [[ -z "$hash" ]]; then
    echo "error: missing checksum for javinizer-windows-amd64.exe in $checksums" >&2
    exit 1
fi

app_hash="$(sha_for 'Javinizer.exe')"
if [[ -z "$app_hash" ]]; then
    echo "error: missing desktop-app checksum (Javinizer.exe) in $checksums" >&2
    echo "  This is required to render the javinizer-app manifest. If this release did" >&2
    echo "  not build the desktop app, the manifest cannot be published." >&2
    exit 1
fi

# Use '|' as the sed delimiter; version (digits/dots) and hash (hex) contain no '|'.
# Both placeholders are substituted on every render; each template only uses the
# one it needs, so unused substitutions are no-ops.
sed \
    -e "s|__VERSION__|${version}|g" \
    -e "s|__HASH__|${hash}|g" \
    -e "s|__APP_HASH__|${app_hash}|g" \
    "$template"
