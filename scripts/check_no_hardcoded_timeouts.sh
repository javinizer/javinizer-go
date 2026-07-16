#!/usr/bin/env bash
set -euo pipefail

ALLOWLIST="internal/timeout/allowlist.txt"
if [ ! -f "$ALLOWLIST" ]; then
  echo "ERROR: allowlist $ALLOWLIST not found" >&2
  exit 1
fi

PATTERN='([^a-zA-Z]Timeout:[[:space:]]*[0-9]+[[:space:]]*\*[[:space:]]*time\.(Second|Minute|Millisecond|Microsecond|Nanosecond)|SetTimeout[[:space:]]*\([[:space:]]*[0-9]+[[:space:]]*\*[[:space:]]*time\.(Second|Minute|Millisecond|Microsecond|Nanosecond))'

violation_found=0
while IFS= read -r match; do
  file=$(echo "$match" | cut -d: -f1)
  file="${file#./}"
  if grep -qx "$file" "$ALLOWLIST"; then
    continue
  fi
  case "$file" in
    *_test.go) continue ;;
  esac
  echo "VIOLATION: hard-coded timeout literal in $match"
  echo "  If this is a one-off admin/utility endpoint with no user config, add $file to $ALLOWLIST."
  echo "  Otherwise, resolve the timeout via internal/timeout.FromConfig()."
  violation_found=1
done < <(grep -rnE "$PATTERN" --include='*.go' . 2>/dev/null || true)

if [ "$violation_found" -eq 1 ]; then
  echo "FAIL: hard-coded timeout literals found outside allowlist" >&2
  exit 1
fi

echo "OK: no hard-coded timeout literals outside allowlist"
