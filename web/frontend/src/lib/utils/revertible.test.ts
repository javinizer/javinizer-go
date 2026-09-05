import { describe, it, expect } from 'vitest';
import { getRevertibleOperationCount } from './revertible';

describe('getRevertibleOperationCount', () => {
	it('subtracts reverted rows from the operation total', () => {
		expect(getRevertibleOperationCount({ operation_count: 5, reverted_count: 2 })).toBe(3);
	});

	it('subtracts terminal noop rows alongside reverted rows (codex P2, PR #241 F2)', () => {
		// A batch of 4: one duplicate skip (noop), one already reverted →
		// exactly 1 operation remains revertible, not 2 as the pre-F2 math
		// (noop-unaware) would have reported.
		expect(getRevertibleOperationCount({ operation_count: 4, reverted_count: 1, noop_count: 2 })).toBe(1);
	});

	it('reads zero when all operations are terminal', () => {
		expect(getRevertibleOperationCount({ operation_count: 3, reverted_count: 2, noop_count: 1 })).toBe(0);
	});

	it('treats a missing noop_count (stale/older payload) as zero', () => {
		expect(getRevertibleOperationCount({ operation_count: 3, reverted_count: 0, noop_count: undefined })).toBe(3);
	});

	it('clamps at zero when counts disagree (never a negative revertible total)', () => {
		expect(getRevertibleOperationCount({ operation_count: 1, reverted_count: 1, noop_count: 1 })).toBe(0);
	});

	it('reports all rows for a freshly organized batch', () => {
		expect(getRevertibleOperationCount({ operation_count: 7, reverted_count: 0, noop_count: 0 })).toBe(7);
	});
});
