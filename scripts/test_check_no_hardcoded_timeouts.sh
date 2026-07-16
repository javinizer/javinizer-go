#!/usr/bin/env bash
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/check_no_hardcoded_timeouts.sh"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

mkdir -p "$TMPDIR/internal/timeout" "$TMPDIR/internal/fake"
cat > "$TMPDIR/internal/timeout/allowlist.txt" <<'EOF'
internal/fake/allowed.go
EOF

cat > "$TMPDIR/internal/fake/violation.go" <<'EOF'
package fake
import "net/http"
import "time"
func f() { _ = &http.Client{Timeout: 30 * time.Second} }
EOF

cd "$TMPDIR"
if bash "$SCRIPT" 2>/dev/null; then
  echo "FAIL: script did not catch single-line violation"
  exit 1
fi
echo "PASS: script caught single-line violation"

rm -f "$TMPDIR/internal/fake/violation.go"

cat > "$TMPDIR/internal/fake/allowed.go" <<'EOF'
package fake
import "net/http"
import "time"
func f() { _ = &http.Client{Timeout: 30 * time.Second} }
EOF

if ! bash "$SCRIPT" 2>/dev/null; then
  echo "FAIL: script flagged an allowlisted file"
  exit 1
fi
echo "PASS: script allowed the allowlisted file"

cat > "$TMPDIR/internal/fake/multiline.go" <<'EOF'
package fake
import "net/http"
import "time"
func f() *http.Client {
  return &http.Client{
    Timeout: 30 * time.Second,
  }
}
EOF
if bash "$SCRIPT" 2>/dev/null; then
  echo "FAIL: script did not catch multi-line violation"
  exit 1
fi
echo "PASS: script caught multi-line violation"

echo "internal/fake/multiline.go" >> "$TMPDIR/internal/timeout/allowlist.txt"

cat > "$TMPDIR/internal/fake/idle.go" <<'EOF'
package fake
import "net/http"
import "time"
func f() *http.Transport {
  return &http.Transport{
    IdleConnTimeout: 30 * time.Second,
  }
}
EOF
if ! bash "$SCRIPT" 2>/dev/null; then
  echo "FAIL: script false-positived IdleConnTimeout"
  exit 1
fi
echo "PASS: script did not flag IdleConnTimeout"

cat > "$TMPDIR/internal/fake/minute.go" <<'EOF'
package fake
import "net/http"
import "time"
func f() *http.Client {
  return &http.Client{Timeout: 2 * time.Minute}
}
EOF
if bash "$SCRIPT" 2>/dev/null; then
  echo "FAIL: script did not catch time.Minute violation"
  exit 1
fi
echo "PASS: script caught time.Minute violation"

echo "internal/fake/minute.go" >> "$TMPDIR/internal/timeout/allowlist.txt"

echo "ALL PASS"
