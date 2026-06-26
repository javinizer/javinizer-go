import { describe, it, expect } from 'vitest';
import { computeJobProgress, isTerminalStatus, nextOrganizeProgress } from './job-progress';
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

describe('isTerminalStatus: organize/update phase-complete aggregates', () => {
	// F-FE-CONS-1: apply_phase.go OnPhaseComplete emits the FINAL aggregate WS
	// frame for a completed organize/update job as status
	// 'organization_completed'/'update_completed' (Progress:100, no file_path),
	// after all per-file 'organized'/'updated' frames and before
	// MarkOrganized/MarkCompleted (which emit no further ProgressMessage). The
	// last WS frame per job is one of these, so they must be terminal — otherwise
	// Home activeJobCount (which folds latestByJob and tests
	// !isTerminalStatus(latest.status)) perpetually counts a completed
	// organize/update job as active until WS reconnect/reload.
	it('returns true for organization_completed', () => {
		expect(isTerminalStatus('organization_completed')).toBe(true);
	});
	it('returns true for update_completed', () => {
		expect(isTerminalStatus('update_completed')).toBe(true);
	});
	it('is case insensitive for the aggregate statuses', () => {
		expect(isTerminalStatus('ORGANIZATION_COMPLETED')).toBe(true);
		expect(isTerminalStatus('UPDATE_COMPLETED')).toBe(true);
	});
	it('models the activeJobCount fix: a latestByJob entry whose latest message has status organization_completed (Progress:100, no file_path) is NOT active', () => {
		// +page.svelte activeJobCount derives `hasActive = !isTerminalStatus(latest.status)`.
		// The final aggregate frame for a completed organize job carries no
		// file_path and Progress:100, so it lives only in latestByJob (not
		// messagesByFile). With this status now terminal, hasActive is false → the
		// job is no longer counted as active. (apply_phase.go OnPhaseComplete.)
		const latest: ProgressMessage = makeMessage({
			status: 'organization_completed',
			progress: 100,
			file_path: '',
		});
		expect(isTerminalStatus(latest.status)).toBe(true);
		expect(!isTerminalStatus(latest.status)).toBe(false);
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

describe('nextOrganizeProgress', () => {
	it('allows an explicit 0 as a reset regardless of current value', () => {
		// prepareOrganizeRun resets the bar to 0 between runs; the guard must not
		// block the reset even when current is already high (e.g. 100 from a prior run).
		expect(nextOrganizeProgress(100, 0)).toBe(0);
		expect(nextOrganizeProgress(50, 0)).toBe(0);
		expect(nextOrganizeProgress(0, 0)).toBe(0);
	});

	it('applies strictly increasing values so the bar advances monotonically', () => {
		expect(nextOrganizeProgress(0, 25)).toBe(25);
		expect(nextOrganizeProgress(25, 50)).toBe(50);
		expect(nextOrganizeProgress(50, 75)).toBe(75);
		expect(nextOrganizeProgress(75, 100)).toBe(100);
	});

	it('ignores values that would move the bar backward', () => {
		expect(nextOrganizeProgress(50, 25)).toBeNull();
		expect(nextOrganizeProgress(100, 1)).toBeNull();
	});

	it('ignores equal values (only strictly greater advances the bar)', () => {
		// Duplicate/out-of-order delivery of the same aggregate value must not
		// re-fire; the backend high-water mutex already prevents this, the guard
		// is defense-in-depth.
		expect(nextOrganizeProgress(50, 50)).toBeNull();
		expect(nextOrganizeProgress(100, 100)).toBeNull();
	});

	it('ignores negative values (defensive; never produced by the backend)', () => {
		expect(nextOrganizeProgress(50, -10)).toBeNull();
		expect(nextOrganizeProgress(0, -1)).toBeNull();
	});

	it('models a full two-run organize sequence: 100 -> 0 (reset) -> 25 -> 50 -> 75 -> 100', () => {
		// Simulate the store applying the guard over a WS message stream: run 1
		// completes at 100, prepareOrganizeRun resets to 0, run 2 advances to 100.
		const sequence = [100, 0, 25, 50, 75, 100];
		const observed: number[] = [];
		let bar = 0;
		for (const next of sequence) {
			const applied = nextOrganizeProgress(bar, next);
			if (applied !== null) bar = applied;
			observed.push(bar);
		}
		expect(observed).toEqual([100, 0, 25, 50, 75, 100]);
	});
});
