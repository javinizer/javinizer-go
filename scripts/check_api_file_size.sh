#!/usr/bin/env bash
set -euo pipefail

max_lines="${1:-700}"
target_dir="${2:-internal/api}"

if ! [[ "${max_lines}" =~ ^[0-9]+$ ]]; then
  echo "max_lines must be an integer, got: ${max_lines}" >&2
  exit 2
fi

if [[ ! -d "${target_dir}" ]]; then
  echo "target_dir does not exist: ${target_dir}" >&2
  exit 2
fi

violations=0

while IFS= read -r -d '' file; do
  line_count="$(wc -l < "${file}" | tr -d ' ')"
  if (( line_count > max_lines )); then
    echo "ERROR: ${file} has ${line_count} lines (max: ${max_lines})"
    violations=1
  fi
done < <(find "${target_dir}" -type f -name '*.go' ! -name '*_test.go' -print0 | sort -z)

if (( violations > 0 )); then
  echo
  echo "Split large files by concern before merging."
  exit 1
fi

echo "OK: all non-test Go files in ${target_dir} are <= ${max_lines} lines."
