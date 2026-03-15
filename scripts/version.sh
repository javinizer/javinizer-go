#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
VERSION_FILE="${REPO_ROOT}/internal/version/version.txt"
DEFAULT_VERSION="v0.0.0-dev"

base_version="${DEFAULT_VERSION}"
if [[ -f "${VERSION_FILE}" ]]; then
	base_version="$(tr -d '[:space:]' < "${VERSION_FILE}")"
fi

if [[ -n "${base_version}" ]] && [[ "${base_version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]]; then
	default_output="${base_version}"
else
	default_output="${DEFAULT_VERSION}"
fi

if command -v git >/dev/null 2>&1 && git -C "${REPO_ROOT}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	tag="$(
		git -C "${REPO_ROOT}" tag --points-at HEAD |
			grep -E '^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$' |
			sort -V |
			tail -n 1 || true
	)"
	if [[ -n "${tag}" ]]; then
		printf '%s\n' "${tag}"
		exit 0
	fi
fi

printf '%s\n' "${default_output}"
