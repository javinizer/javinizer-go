import type { ProgressMessage } from '$lib/api/types';

export const TERMINAL_STATUSES = new Set([
	'completed',
	'failed',
	'cancelled',
	'organized',
	'reverted',
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
