#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
FALLBACK_VERSION="v0.0.0-dev"

if command -v git >/dev/null 2>&1 && git -C "${REPO_ROOT}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	desc="$(git -C "${REPO_ROOT}" describe --tags --match 'v*' --always --abbrev=12 --dirty 2>/dev/null || true)"
	if [[ -n "${desc}" ]]; then
		if [[ "${desc}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
			printf '%s\n' "${desc}"
			exit 0
		fi
		printf '%s\n' "v0.0.0-${desc}"
		exit 0
	fi
fi

printf '%s\n' "${FALLBACK_VERSION}"
