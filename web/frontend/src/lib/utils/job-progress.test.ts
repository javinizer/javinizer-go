import { describe, it, expect } from 'vitest';
import { computeJobProgress, isTerminalStatus } from './job-progress';
import type { ProgressMessage } from '$lib/api/types';

function makeMessage(overrides: Partial<ProgressMessage> = {}): ProgressMessage {
	return {
		job_id: 'job-1',
		file_index: 0,
		file_path: '/path/to/file.mkv',
		status: 'running',
		progress: 0,
		message: '',
		...overrides,
	};
}

describe('isTerminalStatus', () => {
	it('returns true for completed', () => {
		expect(isTerminalStatus('completed')).toBe(true);
	});
	it('returns true for failed', () => {
		expect(isTerminalStatus('failed')).toBe(true);
	});
	it('returns true for cancelled', () => {
		expect(isTerminalStatus('cancelled')).toBe(true);
	});
	it('returns true for organized', () => {
		expect(isTerminalStatus('organized')).toBe(true);
	});
	it('returns true for reverted', () => {
		expect(isTerminalStatus('reverted')).toBe(true);
	});
	it('is case insensitive', () => {
		expect(isTerminalStatus('COMPLETED')).toBe(true);
	});
	it('returns false for running', () => {
		expect(isTerminalStatus('running')).toBe(false);
	});
	it('returns false for pending', () => {
		expect(isTerminalStatus('pending')).toBe(false);
	});
	it('returns false for null', () => {
		expect(isTerminalStatus(null)).toBe(false);
	});
	it('returns false for undefined', () => {
		expect(isTerminalStatus(undefined)).toBe(false);
	});
});

describe('computeJobProgress', () => {
	describe('non-running jobs', () => {
		it('uses finishedCount / totalFiles when not running', () => {
			const result = computeJobProgress({}, 66, 99, false, 31);
			expect(result).toBe(47);
		});

		it('returns 0 when no files finished', () => {
			const result = computeJobProgress({}, 66, 0, false, 0);
			expect(result).toBe(0);
		});

		it('returns 100 when all files finished', () => {
			const result = computeJobProgress({}, 66, 0, false, 66);
			expect(result).toBe(100);
		});

		it('returns restProgress when totalFiles is 0', () => {
			expect(computeJobProgress({}, 0, 42, false, 0)).toBe(42);
		});
	});

	describe('running jobs', () => {
		it('counts finished files at 100% each', () => {
			const messages = {
				a: makeMessage({ file_path: 'a', status: 'running', progress: 50 }),
			};
			const result = computeJobProgress(messages, 66, 0, true, 31);
			expect(result).toBe(48);
		});

		it('includes active file progress in the calculation', () => {
			const messages = {
				a: makeMessage({ file_path: 'a', status: 'running', progress: 50 }),
				b: makeMessage({ file_path: 'b', status: 'running', progress: 50 }),
				c: makeMessage({ file_path: 'c', status: 'running', progress: 50 }),
				d: makeMessage({ file_path: 'd', status: 'running', progress: 50 }),
				e: makeMessage({ file_path: 'e', status: 'running', progress: 50 }),
			};
			const result = computeJobProgress(messages, 66, 0, true, 31);
			expect(result).toBe(51);
		});

		it('caps at 100%', () => {
			const messages = {
				a: makeMessage({ file_path: 'a', status: 'running', progress: 100 }),
				b: makeMessage({ file_path: 'b', status: 'running', progress: 100 }),
				c: makeMessage({ file_path: 'c', status: 'running', progress: 100 }),
				d: makeMessage({ file_path: 'd', status: 'running', progress: 100 }),
				e: makeMessage({ file_path: 'e', status: 'running', progress: 100 }),
			};
			const result = computeJobProgress(messages, 10, 0, true, 8);
			expect(result).toBe(100);
		});

		it('handles no active messages (all queued or finished)', () => {
			const result = computeJobProgress({}, 66, 0, true, 31);
			expect(result).toBe(47);
		});

		it('handles undefined messagesByFile', () => {
			const result = computeJobProgress(undefined, 66, 0, true, 31);
			expect(result).toBe(47);
		});

		it('clamps progress values to 0-100', () => {
			const messages = {
				a: makeMessage({ file_path: 'a', status: 'running', progress: 150 }),
				b: makeMessage({ file_path: 'b', status: 'running', progress: -10 }),
			};
			const result = computeJobProgress(messages, 66, 0, true, 30);
			expect(result).toBe(47);
		});

		it('returns restProgress when totalFiles is 0', () => {
			expect(computeJobProgress({}, 0, 42, true, 0)).toBe(42);
		});
	});

	describe('regression: matches completed items count', () => {
		it('31 finished out of 66 files with 5 active at 100% should be ~55%', () => {
			const messages = {
				a: makeMessage({ file_path: 'a', status: 'running', progress: 100 }),
				b: makeMessage({ file_path: 'b', status: 'running', progress: 100 }),
				c: makeMessage({ file_path: 'c', status: 'running', progress: 100 }),
				d: makeMessage({ file_path: 'd', status: 'running', progress: 100 }),
				e: makeMessage({ file_path: 'e', status: 'running', progress: 100 }),
			};
			const result = computeJobProgress(messages, 66, 0, true, 31);
			expect(result).toBe(55);
		});

		it('31 finished out of 66 files with 5 active at 50% should be ~51%', () => {
			const messages = {
				a: makeMessage({ file_path: 'a', status: 'running', progress: 50 }),
				b: makeMessage({ file_path: 'b', status: 'running', progress: 50 }),
				c: makeMessage({ file_path: 'c', status: 'running', progress: 50 }),
				d: makeMessage({ file_path: 'd', status: 'running', progress: 50 }),
				e: makeMessage({ file_path: 'e', status: 'running', progress: 50 }),
			};
			const result = computeJobProgress(messages, 66, 0, true, 31);
			expect(result).toBe(51);
		});

		it('31 finished out of 66 files with no active should be 47%', () => {
			const result = computeJobProgress({}, 66, 0, true, 31);
			expect(result).toBe(47);
		});
	});
});
