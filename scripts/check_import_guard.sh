#!/usr/bin/env bash
set -euo pipefail

# check_import_guard.sh — Fails if any file in internal/models/ imports internal/config.
# Enforces the dependency direction: config → models (accepted), models → config (forbidden).
# See ADR-0020.

TARGET_DIR="${1:-internal/models}"

if [[ ! -d "${TARGET_DIR}" ]]; then
  echo "target_dir does not exist: ${TARGET_DIR}" >&2
  exit 2
fi

VIOLATIONS=$(grep -rn '"github.com/javinizer/javinizer-go/internal/config"' \
    "${TARGET_DIR}" --include='*.go' 2>/dev/null || true)

if [ -n "$VIOLATIONS" ]; then
  echo "❌ Forbidden import: models → config back-edge detected:"
  echo "$VIOLATIONS"
  echo ""
  echo "The models package must not import config. See ADR-0020."
  exit 1
fi

echo "✅ No models → config back-edge imports found"
