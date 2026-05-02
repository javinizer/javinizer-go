import type { ProgressMessage } from '$lib/api/types';

export const TERMINAL_STATUSES = new Set(['completed', 'failed', 'cancelled', 'organized', 'reverted']);

export function isTerminalStatus(status: string | null | undefined): boolean {
	if (!status) return false;
	return TERMINAL_STATUSES.has(status.toLowerCase());
}

export function computeJobProgress(
	messagesByFile: Record<string, ProgressMessage> | undefined,
	totalFiles: number,
	restProgress: number,
	isRunning: boolean,
): number {
	if (!isRunning) return restProgress;
	if (!messagesByFile) return restProgress;
	const entries = Object.values(messagesByFile);
	if (entries.length === 0) return restProgress;
	const total = Math.max(totalFiles, entries.length);
	if (total === 0) return restProgress;
	let done = 0;
	let runningProgress = 0;
	for (const m of entries) {
		if (isTerminalStatus(m.status)) {
			done++;
		} else {
			runningProgress += Math.max(0, Math.min(100, m.progress));
		}
	}
	return Math.min(Math.round(((done + runningProgress / 100) / total) * 100), 100);
}
