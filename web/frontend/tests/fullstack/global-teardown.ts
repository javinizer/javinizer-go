/**
 * Full-stack global teardown. Both spawned processes (Go backend +
 * Vite dev server) are killed by Playwright's webServer harness when the
 * run exits; the Go binary's main() exits cleanly on SIGTERM + defers its
 * temp DB cleanup. No file fixtures outside
 * /tmp/javinizer-e2e-input + /tmp/javinizer-e2e-output are created, and
 * those are reused across runs (not cleaned here so a subsequent
 * developer run re-uses them and is faster).
 *
 * Declared as the teardown project in playwright.fullstack.config.ts's
 * project list.
 */
import { test as teardownTest } from '@playwright/test';

teardownTest('full-stack teardown', async () => {
	// Intentional no-op.
});
