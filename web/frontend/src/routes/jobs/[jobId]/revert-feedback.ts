import type { OperationListResponse, RevertFileError, RevertResultResponse } from '$lib/api/types';

export interface RevertFeedback {
	jobId: string;
	errors: RevertFileError[];
	// A response can omit details. Keep its attempted scope unresolved until
	// an authoritative operation-list refresh proves every member terminal.
	unconfirmedScopes: number[][];
}

export function mergeRevertFeedback(
	previous: RevertFeedback | null,
	result: RevertResultResponse,
	attemptedIds: number[],
): RevertFeedback {
	const prior = previous?.jobId === result.job_id ? previous : null;
	const errors = new Map(prior?.errors.map((entry) => [entry.operation_id, entry]));
	for (const entry of result.errors ?? []) errors.set(entry.operation_id, entry);
	const unconfirmedScopes = [...(prior?.unconfirmedScopes ?? [])];
	if ((result.errors?.length ?? 0) < result.failed + result.skipped) {
		const scope = [...new Set(attemptedIds)].sort((a, b) => a - b);
		if (!unconfirmedScopes.some((ids) => ids.join(',') === scope.join(',')))
			unconfirmedScopes.push(scope);
	}
	return { jobId: result.job_id, errors: [...errors.values()], unconfirmedScopes };
}

export function reconcileRevertFeedback(
	feedback: RevertFeedback,
	result: RevertResultResponse,
	scopedIds: number[],
	operations: OperationListResponse | undefined,
): RevertFeedback {
	if (!operations || operations.job_id !== feedback.jobId) return feedback;
	// Counts do not identify successful operations. Only a freshly loaded
	// terminal status can resolve a previous error, and only inside this
	// request's attempted/returned scope. Excluded rows retain their details.
	// The caller already combined attempted and returned IDs and removed
	// any IDs superseded by newer responses. Never re-expand that scope.
	const scope = new Set(scopedIds);
	const returnedErrors = new Map((result.errors ?? []).map((entry) => [entry.operation_id, entry]));
	const terminal = new Map(
		operations.operations
			.filter(
				(operation) => operation.revert_status === 'reverted' || operation.revert_status === 'noop',
			)
			.map((operation) => [operation.id, operation.revert_status]),
	);
	const resolved = (id: number) => {
		// Pending-move recovery settles noop but reports skipped/anchor_missing.
		// A fresh authoritative noop resolves that skip. Keep explicit failures
		// conservative even when their snapshot appears terminal.
		const returned = returnedErrors.get(id);
		return (
			scope.has(id) &&
			terminal.has(id) &&
			(!returned || (returned.outcome === 'skipped' && terminal.get(id) === 'noop'))
		);
	};
	return {
		...feedback,
		errors: feedback.errors.filter((entry) => !resolved(entry.operation_id)),
		unconfirmedScopes: feedback.unconfirmedScopes.flatMap((ids) => {
			if (!ids.length) return [ids];
			const remaining = ids.filter((id) => !resolved(id));
			return remaining.length ? [remaining] : [];
		}),
	};
}
