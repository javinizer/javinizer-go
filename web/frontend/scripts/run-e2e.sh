#!/bin/bash
set -e

# E2E Test Runner
# Spawns an isolated backend with a temp database, runs tests, and cleans up.
#
# Prerequisites:
#   - Vite dev server running on port 5174 (make web-dev)
#   - No other backend running on port 8080 (stop Air or your dev backend)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../../../" && pwd)"
TEST_DIR=$(mktemp -d /tmp/javinizer-e2e.XXXXXX)
DB_PATH="$TEST_DIR/test.db"
CONFIG_PATH="$TEST_DIR/config.yaml"
PORT=8080

cleanup() {
  echo "[e2e-runner] Cleaning up..."
  if [[ -n "$E2E_PID" ]]; then
    # Kill process group to catch go run's child binary
    kill -- -"$E2E_PID" 2>/dev/null || { pkill -P "$E2E_PID" 2>/dev/null; kill "$E2E_PID" 2>/dev/null; }
    wait "$E2E_PID" 2>/dev/null
  fi
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

# Check if port 8080 is available
if lsof -ti:$PORT >/dev/null 2>&1; then
  echo "[e2e-runner] ERROR: Port $PORT is already in use."
  echo "[e2e-runner] Stop your dev backend first (kill Air or stop your javinizer api process)."
  echo "[e2e-runner] Running on port $PORT: $(lsof -ti:$PORT | xargs ps -p 2>/dev/null | head -3)"
  exit 1
fi

cd "$PROJECT_DIR"
echo "[e2e-runner] Starting isolated backend on port $PORT..."
echo "[e2e-runner] Temp DB: $DB_PATH"

# Spawn isolated backend
JAVINIZER_DB="$DB_PATH" \
JAVINIZER_CONFIG="$CONFIG_PATH" \
JAVINIZER_E2E_AUTH=true \
JAVINIZER_E2E_USERNAME=admin \
JAVINIZER_E2E_PASSWORD=adminpassword123 \
LOG_LEVEL=error \
go run ./cmd/javinizer api --config "$CONFIG_PATH" &
E2E_PID=$!

echo "[e2e-runner] Backend PID: $E2E_PID"

# Wait for backend to start
echo "[e2e-runner] Waiting for backend to start..."
for i in $(seq 1 30); do
  if curl -sf "http://localhost:$PORT/api/v1/auth/status" >/dev/null 2>&1; then
    echo "[e2e-runner] Backend ready (attempt $i)"
    break
  fi
  if ! kill -0 "$E2E_PID" 2>/dev/null; then
    echo "[e2e-runner] ERROR: Backend process exited unexpectedly."
    exit 1
  fi
  sleep 1
done

if [[ $i -eq 30 ]]; then
  echo "[e2e-runner] ERROR: Backend failed to start within 30 seconds."
  exit 1
fi

# Run e2e tests
echo "[e2e-runner] Running Playwright e2e tests..."
cd "$SCRIPT_DIR/.."
npx playwright test tests/e2e/import-export.spec.ts --reporter=list
EXIT_CODE=$?

echo "[e2e-runner] Tests completed with exit code: $EXIT_CODE"
exit $EXIT_CODE
