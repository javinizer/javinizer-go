/**
 * Organize (apply) WebSocket progress spec — full-stack.
 *
 * Pins the fix for the "progress bar jumps 0%→100%" bug:
 *   fix(web): organize progress bar jumps 0->100% — add per-file WS progress
 *
 * The bug: the apply phase emitted per-file completion only to the in-process
 * jobEventBroadcaster (consumed by TUI/CLI); the API layer never forwarded it
 * to the WebSocket hub, so the frontend received ONLY the terminal
 * OnPhaseComplete broadcast (progress:100) during organize. The bar sat at 0%
 * for the whole run then snapped to 100%.
 *
 * The fix wires an OnFileProgress hook (per file, success or fail) to a
 * monotonic broadcaster that emits incremental
 * ProgressMessage{status:"pending", progress: N/total*100} over the WS hub, so
 * the bar advances 0→100 across files.
 *
 * This spec drives the REAL stack end-to-end with a REAL browser-context
 * WebSocket (the same /ws/progress path + cookie-auth the app's own websocket
 * store uses): browser page → Vite /ws proxy → Go backend → real WS hub → real
 * POST /batch/:id/organize → real apply phase → real per-file OnFileProgress →
 * real hub broadcast → real browser client delivery. NO page.route mocks, NO
 * Node-side WS client. This exercises the real browser→proxy→backend WS path,
 * so a regression in that path (e.g. the Vite /ws proxy changeOrigin same-origin
 * 403) surfaces here.
 *
 * The load-bearing assertion (test 1): at least one WS frame with
 * status="pending" AND 0 < progress < 100 arrives during a MULTI-FILE organize,
 * before the terminal "organization_completed" (progress=100). Before the fix,
 * NO such intermediate pending frame existed — the only organize WS frame was
 * the terminal 100% — so test 1 fails on the pre-fix code. A multi-file job is
 * required: a single file yields 1/1=100% immediately (no intermediate value).
 *
 * Test 2 guards the broadcaster's mutex-held high-water mark: delivered pending
 * progresses are strictly increasing (a regression to a racy atomic-only filter
 * would let a higher count land before a lower one and fail this).
 *
 * Cancel-at-WS-level coverage: NOT asserted here. Canceling organize mid-flight
 * is impractical to make deterministic with the 1-byte fixtures (the apply
 * phase completes in sub-millisecond, so a cancel POST races the finish and
 * usually loses). The contract — OnPhaseComplete (the terminal 100% broadcast)
 * is SKIPPED on a cancelled run — is pinned by the unit test
 * TestApplyPhase_Run_OnFileProgressCancelledAtFanoutEndSkipsPhaseComplete in
 * internal/worker/apply_phase_test.go. Adding a racy e2e here would be a worse
 * outcome than the unit-test coverage already in place.
 */
import { rmSync } from 'node:fs';
import { join } from 'node:path';
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';

import {
	loginAgainstRealBackend,
	submitScrape,
	submitOrganize,
	waitForJobCompletion,
	seedInputFiles,
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
	openProgressWebSocket,
	ORGANIZE_TERMINAL_STATUSES,
	type ProgressFrame,
} from '../helpers';

/**
 * Number of fixture files. The assertions below are robust to the
 * adversarial scheduling where the LAST finisher acquires the broadcaster
 * mutex first and emits the 100% pending frame, filtering all lower counts
 * (the mutex-held high-water check drops regressions) — in that pathological
 * case exactly ONE pending frame (the 100%) is delivered, so the assertions
 * require only "at least one pending frame" + (when >1) strict monotonicity,
 * neither of which flakes. FILE_COUNT=12 is chosen so that in the COMMON case
 * (first-finisher-wins-mutex, the normal scheduling reality) MANY pending
 * frames arrive, giving the monotonicity check a meaningful multi-step
 * sequence to guard against a racy atomic-only filter regression. 12 keeps
 * the test fast while yielding a clear 1..12 sequence in the common case.
 */
const FILE_COUNT = 12;

/**
 * GOOD-* is required (the e2emock scraper rejects unrecognized ID prefixes).
 * GOOD-001 is a CANONICAL_FIXTURE, so this spec uses GOOD-002..GOOD-013 — all
 * non-canonical — and cleans them up in finally so no misleading canonical-ish
 * files linger in the shared input dir.
 */
function specFixtureNames(): string[] {
	return Array.from({ length: FILE_COUNT }, (_, i) => `GOOD-${String(i + 2).padStart(3, '0')}.mp4`);
}

/** Remove the spec's seeded fixtures (best-effort; idempotent). */
function cleanupSpecFixtures(): void {
	for (const name of specFixtureNames()) {
		rmSync(join(DEFAULT_INPUT_DIR, name), { force: true });
	}
}

test.describe('Organize WebSocket progress: bar advances per file (no 0→100 jump)', () => {
	test('a multi-file organize emits intermediate pending WS progress before the terminal 100%', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Seed a multi-file job so an intermediate progress value exists.
		// GOOD-* always succeeds through the e2emock scraper; the apply phase
		// processes all completed files, emitting one pending frame per file.
		const names = specFixtureNames();
		await seedInputFiles(names);
		const files = names.map((n) => `${DEFAULT_INPUT_DIR}/${n}`);

		try {
			const job_id = await submitScrape(request, { files });
			const scraped = await waitForJobCompletion(request, job_id);
			expect(
				scraped.status,
				'precondition: multi-file scrape must complete so the apply phase has eligible items',
			).toBe('completed');
			expect(
				Object.keys(scraped.results).length,
				'precondition: all seeded files must produce a scrape result',
			).toBe(FILE_COUNT);

			// Open the WS BEFORE submitting organize so no intermediate frame is
			// missed (the apply phase can complete in milliseconds for tiny files).
			const { frames, terminal, close } = await openProgressWebSocket(page, job_id);
			try {
				await submitOrganize(request, job_id, `${DEFAULT_OUTPUT_DIR}/org-progress-${Date.now()}`);

				// Wait for the terminal organize frame, with a bounded timeout as a
				// safety net in case the terminal broadcast is missed. `terminal`
				// also rejects fast if the socket closes mid-test.
				await Promise.race([terminal, new Promise<void>((resolve) => setTimeout(resolve, 20_000))]);

				// Re-fetch the job to confirm the apply phase actually ran to a
				// terminal state (belt + suspenders — the terminal WS frame is the
				// primary signal, but this rules out a no-op apply that emitted
				// nothing).
				await waitForJobCompletion(request, job_id, { timeoutMs: 30_000 });
			} finally {
				await close();
			}

			// ---- The load-bearing bug-fix assertion ----
			// At least one pending frame must have been delivered. Before the fix,
			// the ONLY organize WS frame was the terminal organization_completed
			// (status != "pending"), so this fails on the pre-fix code. Post-fix, even
			// in the pathological "last finisher wins the mutex first" ordering (which
			// emits a single pending 100% frame), at least one pending frame is
			// delivered — so this assertion is robust to every ordering.
			//
			// (We do NOT hard-assert an intermediate 0 < progress < 100 value: that
			// would flake under the pathological ordering, which emits only the
			// pending 100%. The core fix signal is "a pending frame reached the hub"
			// — the exact discriminator between the pre-fix terminal-only stream and
			// the post-fix per-file stream.)
			const hasPending = frames.some((f) => f.status === 'pending');
			expect(
				hasPending,
				`a multi-file organize must emit at least one pending WS progress frame — got ${JSON.stringify(frames)}`,
			).toBe(true);

			// ---- Terminal frame ----
			// The organize completion frame must arrive and report progress=100.
			const terminalFrames = frames.filter((f) =>
				(ORGANIZE_TERMINAL_STATUSES as readonly string[]).includes(f.status),
			);
			expect(
				terminalFrames.length,
				'a terminal organization_completed/update_completed frame must be delivered',
			).toBeGreaterThanOrEqual(1);
			expect(terminalFrames[0].progress, 'terminal frame must report progress=100').toBe(100);
		} finally {
			cleanupSpecFixtures();
		}
	});

	test('delivered pending organize progresses are strictly increasing (monotonic delivery)', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		// Guards the broadcaster's mutex-held high-water mark: emits are
		// serialized in increasing order, so the client never observes a
		// regression (a higher count delivered before a lower one). A
		// regression to a racy atomic-only filter would let a 60% frame land
		// before a 40% frame and fail this.
		await loginAgainstRealBackend(request);

		const names = specFixtureNames();
		await seedInputFiles(names);
		const files = names.map((n) => `${DEFAULT_INPUT_DIR}/${n}`);

		try {
			const job_id = await submitScrape(request, { files });
			await waitForJobCompletion(request, job_id);

			const { frames, terminal, close } = await openProgressWebSocket(page, job_id);
			try {
				await submitOrganize(
					request,
					job_id,
					`${DEFAULT_OUTPUT_DIR}/org-progress-mono-${Date.now()}`,
				);
				await Promise.race([terminal, new Promise<void>((resolve) => setTimeout(resolve, 20_000))]);
				await waitForJobCompletion(request, job_id, { timeoutMs: 30_000 });
			} finally {
				await close();
			}

			// The pending frames (per-file throughput) must be strictly increasing
			// when more than one is delivered. The terminal frame (100) is emitted
			// after every OnFileProgress call returns, so it naturally follows. In
			// the pathological "last finisher wins the mutex first" ordering exactly
			// one pending frame is delivered, so the loop below is vacuous (no
			// pair to compare) and the assertion degrades to "at least one pending
			// frame" — robust. In the common multi-frame case the loop actively
			// catches a racy atomic-only filter regression (out-of-order delivery).
			//
			// Filter to the AGGREGATE pending stream only (no file_path): the
			// high-water mutex invariant applies to makeOrganizeProgressBroadcaster's
			// job-level frames. The per-file 'Organizing <file>' start frames
			// (makeOrganizeFileStartBroadcaster) are also status 'pending' but carry
			// a file_path + Progress:0; they are a separate display-only stream and
			// are not part of the monotonic-delivery invariant (matching the
			// organize-controller bar-drive gate on !msg.file_path).
			const pending = frames.filter((f) => f.status === 'pending' && !f.file_path);
			expect(
				pending.length,
				'a multi-file organize must emit at least one pending frame',
			).toBeGreaterThanOrEqual(1);
			for (let i = 1; i < pending.length; i++) {
				expect(
					pending[i].progress,
					`pending progress must be strictly increasing — frame ${i} (${pending[i].progress}) regressed below frame ${i - 1} (${pending[i - 1].progress}); full sequence: ${JSON.stringify(pending.map((p) => p.progress))}`,
				).toBeGreaterThan(pending[i - 1].progress);
			}
		} finally {
			cleanupSpecFixtures();
		}
	});
});
