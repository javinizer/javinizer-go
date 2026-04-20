#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ ! -f "$repo_root/web/frontend/build/index.html" ]; then
	echo "missing web/frontend/build/index.html; run make web-build first" >&2
	exit 1
fi

restore_placeholder() {
	git -C "$repo_root" checkout -- web/dist/
}

trap restore_placeholder EXIT

rm -rf "$repo_root/web/dist"
mkdir -p "$repo_root/web/dist"
cp -R "$repo_root/web/frontend/build/." "$repo_root/web/dist/"

cd "$repo_root"
"$@"
