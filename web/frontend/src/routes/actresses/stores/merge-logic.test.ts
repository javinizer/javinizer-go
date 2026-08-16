import { describe, expect, it } from 'vitest';
import { isRetryableActressMergeError } from './merge-logic';

describe('isRetryableActressMergeError', () => {
	it('recognizes stale merge plans', () => {
		expect(isRetryableActressMergeError({ code: 'ACTRESS_MERGE_STALE_PLAN' })).toBe(true);
	});

	it('rejects non-retryable errors', () => {
		expect(isRetryableActressMergeError(new Error('merge failed'))).toBe(false);
		expect(isRetryableActressMergeError({ code: 'ACTRESS_MERGE_CONFLICT' })).toBe(false);
		expect(isRetryableActressMergeError(null)).toBe(false);
	});
});
