import type { BatchJobResponse, FileResult } from '$lib/api/types';
import type { FileStatus } from './organize-controller';

export type CreditFailureKind = 'conflict' | 'unavailable';
export interface CreditGuidance {
	kind: CreditFailureKind;
	movieIndex: number | null;
}

export function getCreditFailureKind(result: FileResult): CreditFailureKind | null {
	if (result.status !== 'failed' || !result.movie) return null;
	if (result.error_code === 'credit_conflict') return 'conflict';
	if (result.error_code === 'credit_status_unavailable') return 'unavailable';
	// Only recognize the exact, metadata-bearing terminal diagnostics persisted by v1.6.2.
	if (
		result.error_code !== 'blocked' ||
		!result.movie.code ||
		!result.file_path ||
		!result.ended_at
	)
		return null;
	if (result.error === 'apply blocked by an open credit collision') return 'conflict';
	if (result.error === 'apply blocked because credit collision status is unavailable')
		return 'unavailable';
	return null;
}

export function getCreditGuidance(
	filePath: string,
	status: FileStatus,
	job: BatchJobResponse | null,
	groups: { results: FileResult[]; primaryResult: FileResult }[],
): CreditGuidance | null {
	if (status.status !== 'failed') return null;
	const result = job?.results[filePath];
	// The card describes a previous attempt, not the current collision state.
	// Do not borrow a newer result's diagnostic for a different recorded failure.
	const kind =
		status.error_code === 'credit_conflict'
			? 'conflict'
			: status.error_code === 'credit_status_unavailable'
				? 'unavailable'
				: result && result.error === status.error
					? getCreditFailureKind(result)
					: null;
	if (!kind) return null;
	const index = groups.findIndex(
		(group) =>
			!!group.primaryResult.movie?.code && group.results.some((row) => row.file_path === filePath),
	);
	return {
		kind,
		movieIndex: result?.movie && !job?.excluded?.[filePath] && index >= 0 ? index : null,
	};
}

/** Wait for Detail to mount, but never steal focus after a newer navigation. */
export async function focusCreditGuidance(
	afterRender: () => Promise<void>,
	isCurrent: () => boolean,
	getRegion: () => HTMLElement | undefined,
): Promise<void> {
	await afterRender();
	if (!isCurrent()) return;
	const region = getRegion();
	region?.focus({ preventScroll: true });
	region?.scrollIntoView({ block: 'center', behavior: 'smooth' });
}
