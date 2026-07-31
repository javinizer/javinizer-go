import { describe, expect, it } from 'vitest';
import type { ActressSyncJob, ActressSyncTask } from '$lib/api/types';
import { buildActressSyncSummary, isActressSyncTerminal } from './sync-runner';

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

	it('recognizes durable terminal states', () => {
		expect(isActressSyncTerminal(job)).toBe(false);
		expect(isActressSyncTerminal({ ...job, status: 'cancelled' })).toBe(true);
	});
});
