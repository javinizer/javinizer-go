import { describe, expect, it } from 'vitest';
import type {
	OperationItem,
	OperationListResponse,
	RevertFileError,
	RevertResultResponse,
} from '$lib/api/types';
import { mergeRevertFeedback, reconcileRevertFeedback } from './revert-feedback';

const error = (id: number, movie = 'A', message = `problem-${id}`): RevertFileError => ({
	operation_id: id,
	movie_id: movie,
	original_path: `/in/${id}`,
	new_path: `/out/${id}`,
	error: message,
	outcome: 'failed',
	reason: 'unexpected_path_state',
});
const result = (
	errors: RevertFileError[] = [],
	overrides: Partial<RevertResultResponse> = {},
): RevertResultResponse => ({
	job_id: 'job',
	status: 'organized',
	total: 1,
	succeeded: errors.length ? 0 : 1,
	failed: errors.length,
	skipped: 0,
	errors,
	...overrides,
});
const rows = (states: Record<number, string>): OperationListResponse => ({
	job_id: 'job',
	job_status: 'organized',
	total: Object.keys(states).length,
	operations: Object.entries(states).map(
		([id, revert_status]) => ({ id: Number(id), revert_status }) as OperationItem,
	),
});

describe('scoped unresolved Undo feedback', () => {
	it('updates repeated error identity without losing another movie’s error', () => {
		const old = mergeRevertFeedback(null, result([error(1), error(2, 'B')]), [1, 2]);
		const next = mergeRevertFeedback(old, result([error(1, 'A', 'new reason')]), [1]);
		expect(next.errors).toEqual([error(1, 'A', 'new reason'), error(2, 'B')]);
	});
	it('clears only terminal attempted IDs, even if an excluded ID is also terminal', () => {
		const old = mergeRevertFeedback(null, result([error(1), error(2, 'B')]), [1, 2]);
		expect(
			reconcileRevertFeedback(old, result(), [1], rows({ 1: 'reverted', 2: 'reverted' })).errors,
		).toEqual([error(2, 'B')]);
	});
	it('uses returned operation identities for multipart partial success, never response counts alone', () => {
		const old = mergeRevertFeedback(null, result([error(1), error(2), error(3, 'B')]), [1, 2, 3]);
		const response = result([error(2, 'A', 'still blocked')], { total: 2, succeeded: 1 });
		const merged = mergeRevertFeedback(old, response, [1, 2]);
		expect(
			reconcileRevertFeedback(
				merged,
				response,
				[1, 2],
				rows({ 1: 'reverted', 2: 'failed', 3: 'failed' }),
			).errors,
		).toEqual([error(2, 'A', 'still blocked'), error(3, 'B')]);
	});
	it('retains prior details when status refresh is absent, stale, or for another job', () => {
		const old = mergeRevertFeedback(null, result([error(1)]), [1]);
		for (const snapshot of [
			undefined,
			rows({ 1: 'failed' }),
			rows({}),
			{ ...rows({ 1: 'reverted' }), job_id: 'another' },
		]) {
			expect(reconcileRevertFeedback(old, result(), [1], snapshot).errors).toEqual([error(1)]);
		}
	});
	it('recognizes an attempted noop as terminal while preserving returned errors', () => {
		const old = mergeRevertFeedback(null, result([error(1)]), [1]);
		expect(reconcileRevertFeedback(old, result(), [1], rows({ 1: 'noop' })).errors).toEqual([]);
		expect(
			reconcileRevertFeedback(old, result([error(1)]), [1], rows({ 1: 'reverted' })).errors,
		).toEqual([error(1)]);
	});
	it('keeps detail-less outcomes conservative and resolves their scope across individual retries', () => {
		const response = result([], { total: 2, succeeded: 0, failed: 2 });
		let state = mergeRevertFeedback(null, response, [1, 2]);
		state = mergeRevertFeedback(state, response, [2, 1]);
		expect(state.unconfirmedScopes).toEqual([[1, 2]]);
		state = reconcileRevertFeedback(state, result(), [1], rows({ 1: 'reverted', 2: 'failed' }));
		expect(state.unconfirmedScopes).toEqual([[2]]);
		state = reconcileRevertFeedback(state, result(), [2], rows({ 1: 'reverted', 2: 'reverted' }));
		expect(state.unconfirmedScopes).toEqual([]);
	});
	it('does not carry errors across job namespaces', () => {
		const old = mergeRevertFeedback(null, result([error(1)]), [1]);
		expect(mergeRevertFeedback(old, { ...result(), job_id: 'new-job' }, [1]).errors).toEqual([]);
	});
});
