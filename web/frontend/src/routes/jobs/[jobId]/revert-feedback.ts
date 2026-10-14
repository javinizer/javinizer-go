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
	attemptedIds: number[],
	operations: OperationListResponse | undefined,
): RevertFeedback {
	if (!operations || operations.job_id !== feedback.jobId) return feedback;
	// Counts do not identify successful operations. Only a freshly loaded
	// terminal status can resolve a previous error, and only inside this
	// request's attempted/returned scope. Excluded rows retain their details.
	const scope = new Set([
		...attemptedIds,
		...(result.errors ?? []).map((entry) => entry.operation_id),
	]);
	const returnedErrors = new Set((result.errors ?? []).map((entry) => entry.operation_id));
	const terminal = new Set(
		operations.operations
			.filter(
				(operation) => operation.revert_status === 'reverted' || operation.revert_status === 'noop',
			)
			.map((operation) => operation.id),
	);
	const resolved = (id: number) => scope.has(id) && terminal.has(id) && !returnedErrors.has(id);
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
