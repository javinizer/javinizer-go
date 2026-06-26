import type { ProgressMessage } from '$lib/api/types';

export const TERMINAL_STATUSES = new Set([
	'completed',
	'failed',
	'cancelled',
	'organized',
	// 'updated' is the update-mode per-file completion status (symmetric with
	// 'organized' for organize mode; apply_config_builder.go emits it WITH
	// FilePath + Progress:100). Without it here, completed update jobs are
	// perpetually counted in Home 'Active jobs: N' (activeJobCount) until WS
	// reconnect. Excluded from computeJobProgress activeProgress like other
	// terminal per-file statuses (already counted in finishedCount).
	'updated',
	// Aggregate phase-complete statuses emitted by apply_phase.go
	// OnPhaseComplete as the FINAL WS frame for a completed organize/update job
	// (Progress:100, no file_path), after all per-file 'organized'/'updated'
	// frames and before MarkOrganized/MarkCompleted (which emit NO further
	// ProgressMessage). The last WS frame per job is one of these, so they MUST
	// be terminal — otherwise Home activeJobCount (which folds latestByJob and
	// tests !isTerminalStatus(latest.status)) perpetually counts a completed
	// organize/update job as active until WS reconnect/reload (F-FE-CONS-1).
	'organization_completed',
	'update_completed',
	'reverted',
	// Per-file scrape completion statuses. A finished scrape file (success or
	// error) is terminal for that file: it is already counted in finishedCount,
	// so it must NOT also contribute to activeProgress (would double-count and
	// drive the bar to 100% at ~50% completion). Mirrors main, which rendered
	// job.progress directly without summing per-file WS progress.
	'success',
	'error',
	// Per-movie bulk-rescrape terminal outcomes. 'gone' (movie deleted/reached
	// terminal mid-rescrape) and 'conflict' (revision conflict / concurrent
	// modification) are final rescrape results, not in-progress states. Without
	// these here, BulkRescrapeProgress's allDone predicate never becomes true for
	// a rescrape containing gone/conflict movies (stuck undismissable modal) and
	// activeJobCount perpetually counts the finished job as active.
	'gone',
	'conflict',
]);

export function isTerminalStatus(status: string | null | undefined): boolean {
	if (!status) return false;
	return TERMINAL_STATUSES.has(status.toLowerCase());
}

/**
 * Compute overall job progress as a percentage (0–100).
 *
 * @param messagesByFile - Per-file progress messages (keyed by file path)
 * @param totalFiles - Total number of files in the job
 * @param restProgress - Fallback progress when totalFiles is 0
 * @param isRunning - Whether the job is currently running
 * @param finishedCount - Number of files that have finished (terminal status or externally tracked)
 */
export function computeJobProgress(
	messagesByFile: Record<string, ProgressMessage> | undefined,
	totalFiles: number,
	restProgress: number,
	isRunning: boolean,
	finishedCount: number,
): number {
	if (totalFiles === 0) return restProgress;

	if (!isRunning) {
		// Cap at 100%: late-arriving results after completion can make
		// finishedCount exceed totalFiles. The isRunning branch below is already
		// capped; this restores parity so the internal invariant holds (the
		// /jobs display additionally clamps, but callers shouldn't rely on that).
		return Math.min(Math.round((finishedCount / totalFiles) * 100), 100);
	}

	if (!messagesByFile || Object.keys(messagesByFile).length === 0) {
		return Math.min(Math.round((finishedCount / totalFiles) * 100), 100);
	}

	let activeProgress = 0;
	for (const m of Object.values(messagesByFile)) {
		if (!isTerminalStatus(m.status)) {
			activeProgress += Math.max(0, Math.min(100, m.progress));
		}
	}

	const total = Math.max(totalFiles, 1);
	return Math.min(Math.round(((finishedCount + activeProgress / 100) / total) * 100), 100);
}

/**
 * Apply the organize-bar monotonic high-water guard.
 *
 * The review-page OrganizeStatusCard bar must never move backward during a
 * run. Aggregate progress-stream messages are already monotonic (backend
 * high-water mutex), but this is the store-level defense-in-depth against any
 * per-file message that slips through the controller's `!msg.file_path` bar-drive
 * filter and out-of-order delivery.
 *
 * @param current - The bar's current progress (0–100).
 * @param next - The candidate progress value from an incoming WS message.
 * @returns `next` when it should be applied, or `null` when it must be ignored.
 *   An explicit `0` is always returned (it is the reset signal emitted by
 *   `prepareOrganizeRun` between runs); any other value is returned only when
 *   strictly greater than `current`, so the bar never moves backward mid-run.
 */
export function nextOrganizeProgress(current: number, next: number): number | null {
	if (next === 0 || next > current) return next;
	return null;
}
