import { describe, expect, it } from 'vitest';
import type { ActressSyncJob, ActressSyncTask } from '$lib/api/types';
import { appendActressSyncJob, buildActressSyncSummary, isActressSyncJobNotFound, isActressSyncTerminal, loadActressSyncSnapshot, loadActiveActressSyncJobs, mergeActiveActressSyncJobs, orderActiveActressSyncJobs } from './sync-runner';

const job: ActressSyncJob = {
	id: 'job',
	status: 'running',
	scope: 'missing',
	total_tasks: 3,
	completed: 2,
	updated: 1,
	warnings: 1,
	skipped: 1,
	conflicts: 0,
	failed: 0,
	cancelled: 0,
	cancel_requested: false,
	created_at: '',
};

function task(id: string, status: ActressSyncTask['status']): ActressSyncTask {
	return {
		id,
		job_id: 'job',
		label: id,
		dedupe_key: id,
		status,
		stage: status,
		messages: [],
		updated_fields: [],
		attempts: 1,
		created_at: '',
	};
}

describe('actress sync summary', () => {
	it('separates active tasks and diagnostics', () => {
		const summary = buildActressSyncSummary(job, [
			task('active', 'running'),
			task('skip', 'skipped'),
			task('done', 'completed'),
		]);
		expect(summary.active.map((item) => item.id)).toEqual(['active']);
		expect(summary.diagnostics.map((item) => item.id)).toEqual(['skip']);
		expect(summary.processed).toBe(2);
	});

	it('loads job and task state for live polling', async () => {
		const calls: string[] = [];
		let jobLoaded = false;
		const active = task('active', 'running');
		const snapshot = await loadActressSyncSnapshot({
			async getActressSyncJob(jobID) { calls.push(`job:${jobID}`); jobLoaded = true; return { job }; },
			async listActressSyncJobTasks(jobID, view) { expect(jobLoaded).toBe(true); calls.push(`tasks:${jobID}:${view}`); return { tasks: [active] }; },
			async listActiveActressSyncJobs() { return { jobs: [job] }; },
		}, job.id);
		expect(calls).toEqual(['job:job', 'tasks:job:active']);
		expect(snapshot).toEqual({ job, tasks: [active] });
	});

	it('loads bounded diagnostics after a job reaches a terminal state', async () => {
		const terminalJob = { ...job, status: 'completed' as const };
		const diagnostic = task('diagnostic', 'failed');
		let requestedView = '';
		const snapshot = await loadActressSyncSnapshot({
			async getActressSyncJob() { return { job: terminalJob }; },
			async listActressSyncJobTasks(_jobID, view) { requestedView = view ?? ''; return { tasks: [diagnostic] }; },
			async listActiveActressSyncJobs() { return { jobs: [] }; },
		}, terminalJob.id);
		expect(requestedView).toBe('diagnostics');
		expect(snapshot).toEqual({ job: terminalJob, tasks: [diagnostic] });
	});

	it('selects the oldest active job and handles an empty queue', async () => {
		const newer = { ...job, id: 'newer' };
		const client = {
			async getActressSyncJob() { return { job }; },
			async listActressSyncJobTasks() { return { tasks: [] }; },
			async listActiveActressSyncJobs() { return { jobs: [job, newer] }; },
		};
		const active = await loadActiveActressSyncJobs(client);
		expect(active).toEqual([job, newer]);
		expect(orderActiveActressSyncJobs(active)).toEqual({ current: job, queued: [newer] });
		expect(orderActiveActressSyncJobs([])).toEqual({ current: null, queued: [] });
		const concurrent = { ...job, id: 'concurrent' };
		expect(mergeActiveActressSyncJobs(job, [newer], [job, newer, concurrent])).toEqual([newer, concurrent]);
	});

	it('retains queued jobs absent from the active snapshot until displayed', () => {
		// A queued job that finished while another is displayed leaves the
		// server's active-only list but must survive reconciliation; pruned
		// jobs are dropped via the 404 path when the queue advances to them.
		const finished = { ...job, id: 'finished' };
		const keptJob = { ...job, id: 'kept' };
		const added = { ...job, id: 'added' };
		expect(mergeActiveActressSyncJobs(job, [finished, keptJob], [job, keptJob, added])).toEqual([finished, keptJob, added]);
	});

	it('appends a locally created job without reconciling the queue', () => {
		const queued = { ...job, id: 'queued' };
		const created = { ...job, id: 'created' };
		expect(appendActressSyncJob(job, [queued], created)).toEqual([queued, created]);
		expect(appendActressSyncJob(job, [queued], queued)).toEqual([queued]);
		expect(appendActressSyncJob(job, [queued], job)).toEqual([queued]);
	});

	it('detects a deleted job from 404 poll failures', () => {
		expect(isActressSyncJobNotFound({ status: 404 })).toBe(true);
		expect(isActressSyncJobNotFound({ status: 500 })).toBe(false);
		expect(isActressSyncJobNotFound(new Error('boom'))).toBe(false);
		expect(isActressSyncJobNotFound(null)).toBe(false);
	});

	it('recognizes durable terminal states', () => {
		expect(isActressSyncTerminal(job)).toBe(false);
		expect(isActressSyncTerminal({ ...job, status: 'cancelled' })).toBe(true);
	});
});
