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
	it('returns true for updated (update-mode per-file completion)', () => {
		expect(isTerminalStatus('updated')).toBe(true);
	});
	it('returns true for reverted', () => {
		expect(isTerminalStatus('reverted')).toBe(true);
	});
	it('returns true for success (per-file scrape completion)', () => {
		expect(isTerminalStatus('success')).toBe(true);
	});
	it('returns true for error (per-file scrape failure)', () => {
		expect(isTerminalStatus('error')).toBe(true);
	});
	it('returns true for gone (per-movie rescrape terminal outcome)', () => {
		expect(isTerminalStatus('gone')).toBe(true);
	});
	it('returns true for conflict (per-movie rescrape terminal outcome)', () => {
		expect(isTerminalStatus('conflict')).toBe(true);
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

		it('caps at 100 when finishedCount exceeds totalFiles (late-arriving results)', () => {
			// Regression for cycle-1 MINOR #3: the !isRunning branch had no
			// Math.min(...,100) cap, so late-arriving results after completion
			// could yield >100%. The isRunning branch was already capped.
			const result = computeJobProgress({}, 10, 0, false, 12);
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

		it('caps at 100 when finishedCount exceeds totalFiles with no active messages', () => {
			// Regression for cycle-3 F-A: the isRunning + no-active-messages branch
			// (job-progress.ts:43) was uncapped, unlike the active-progress branch.
			// Late-arriving results could yield >100%, rendered unclamped by
			// ProgressModal. Now capped for parity with the other branches.
			const result = computeJobProgress({}, 10, 0, true, 12);
			expect(result).toBe(100);
		});

		it('clamps progress values to 0-100', () => {
			const messages = {
				a: makeMessage({ file_path: 'a', status: 'running', progress: 150 }),
				b: makeMessage({ file_path: 'b', status: 'running', progress: -10 }),
			};
			const result = computeJobProgress(messages, 66, 0, true, 30);
			expect(result).toBe(47);
		});

		it('does NOT double-count finished scrape files (success/error are terminal)', () => {
			// Regression for NEW-2: per-file scrape success/error messages carry
			// progress 100 but are terminal, so they must be excluded from
			// activeProgress (they're already in finishedCount). Before the fix,
			// 5 finished files + 5 success msgs at 100 -> (5 + 5)/10 = 100% at
			// 50% completion. After: (5 + 0)/10 = 50%.
			const messages = {
				a: makeMessage({ file_path: 'a', status: 'success', progress: 100 }),
				b: makeMessage({ file_path: 'b', status: 'success', progress: 100 }),
				c: makeMessage({ file_path: 'c', status: 'success', progress: 100 }),
				d: makeMessage({ file_path: 'd', status: 'success', progress: 100 }),
				e: makeMessage({ file_path: 'e', status: 'error', progress: 100 }),
			};
			const result = computeJobProgress(messages, 10, 0, true, 5);
			// 5 finished files / 10 total = 50%. Before the fix, success/error
			// were non-terminal so each added 1.0 to activeProgress -> (5+5)/10 = 100%.
			expect(result).toBe(50);
		});

		it('returns restProgress when totalFiles is 0', () => {
			expect(computeJobProgress({}, 0, 42, true, 0)).toBe(42);
		});
	});

	describe('organize monotonic bar (iter-6 MAJOR regression guard)', () => {
		// The reverted iter-6 fix (30e6e53f) derived the Home organize bar via
		// computeJobProgress using msgs.length / Object.values(files).length as the
		// total. For organize, messagesByFile holds only terminal per-file
		// 'organized'/'updated' messages (Progress:100), so after the first file:
		// finishedCount=1, total=1 -> bar pegged at 100% for the ENTIRE remaining
		// job. The fix enriches the WS protocol with AUTHORITATIVE total_files
		// (batch.stampJobCounts), so computeJobProgress receives the real total.
		// These tests pin that the bar advances 10->20->...->100 (NOT pegged) and
		// is monotonic non-decreasing, using authoritative totalFiles/finishedCount.
		const organizeFiles = Array.from({ length: 10 }, (_, i) => `f${i + 1}.mp4`);

		function organizeMessages(done: number): Record<string, ProgressMessage> {
			// 'done' terminal files at Progress:100 (organized) + one in-flight
			// 'Organizing <file>' pending file at Progress:0 (the verbose per-file
		// start message from OnFileOrganizeStart). Mirrors the live messagesByFile.
			const msgs: Record<string, ProgressMessage> = {};
			for (let i = 0; i < done; i++) {
				msgs[organizeFiles[i]] = makeMessage({
					file_path: organizeFiles[i],
					status: 'organized',
					progress: 100,
				});
			}
			if (done < 10) {
				msgs[organizeFiles[done]] = makeMessage({
					file_path: organizeFiles[done],
					status: 'pending',
					progress: 0,
				});
			}
			return msgs;
		}

		it('1 of 10 files done -> 10%, NOT 100% (the iter-6 MAJOR)', () => {
			const messages = organizeMessages(1);
			const result = computeJobProgress(messages, 10, 0, true, 1);
			expect(result).toBe(10);
		});

		it('does not peg at 100% mid-job (5 of 10 done -> 50%)', () => {
			const messages = organizeMessages(5);
			const result = computeJobProgress(messages, 10, 0, true, 5);
			expect(result).toBe(50);
		});

		it('organize bar is monotonic non-decreasing 0->100 across 10 files', () => {
			let prev = 0;
			for (let done = 1; done <= 10; done++) {
				const isRunning = done < 10;
			const result = computeJobProgress(organizeMessages(done), 10, 0, isRunning, done);
			expect(result).toBeGreaterThanOrEqual(prev);
			prev = result;
			}
			expect(prev).toBe(100);
		});

		it('reaches 100% only at completion (all 10 terminal, not running)', () => {
			const messages: Record<string, ProgressMessage> = {};
			for (const f of organizeFiles) {
				messages[f] = makeMessage({ file_path: f, status: 'organized', progress: 100 });
			}
			const result = computeJobProgress(messages, 10, 100, false, 10);
			expect(result).toBe(100);
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
