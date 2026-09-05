import type { BatchJobResponse } from '$lib/api/types';

// codex P2 (PR #241 F2): how many of a job's persisted operations can still be
// reverted. Completed-noop rows (authorized intra-batch duplicate skips) are
// TERMINAL and non-revertible exactly like reverted rows, so they are
// subtracted too — before the noop status existed, they leaked into the
// revertible total (operation_count − reverted only), overstating the count
// shown in the batch revert confirmation.
export function getRevertibleOperationCount(
	job: Pick<BatchJobResponse, 'operation_count' | 'reverted_count'> & { noop_count?: number }
): number {
	return Math.max(0, job.operation_count - (job.reverted_count ?? 0) - (job.noop_count ?? 0));
}
