import type { ActressSyncJob, ActressSyncTask } from '$lib/api/types';

export interface ActressSyncSnapshotClient {
	getActressSyncJob(jobID: string): Promise<{ job: ActressSyncJob }>;
	listActressSyncJobTasks(jobID: string, view?: 'active' | 'diagnostics'): Promise<{ tasks: ActressSyncTask[] }>;
	listActiveActressSyncJobs(): Promise<{ jobs: ActressSyncJob[] }>;
}

export interface ActressSyncSummary {
	total: number;
	processed: number;
	updated: number;
	warnings: number;
	skipped: number;
	conflicts: number;
	failed: number;
	cancelled: number;
	active: ActressSyncTask[];
	diagnostics: ActressSyncTask[];
}

export async function loadActressSyncSnapshot(client: ActressSyncSnapshotClient, jobID: string) {
	const jobResponse = await client.getActressSyncJob(jobID);
	const view = isActressSyncTerminal(jobResponse.job) ? 'diagnostics' : 'active';
	const taskResponse = await client.listActressSyncJobTasks(jobID, view);
	return { job: jobResponse.job, tasks: taskResponse.tasks };
}

export async function loadActiveActressSyncJobs(client: ActressSyncSnapshotClient) {
	const response = await client.listActiveActressSyncJobs();
	return response.jobs;
}

export function orderActiveActressSyncJobs(jobs: ActressSyncJob[]) {
	return { current: jobs[0] ?? null, queued: jobs.slice(1) };
}

export function mergeActiveActressSyncJobs(current: ActressSyncJob | null, queued: ActressSyncJob[], active: ActressSyncJob[]) {
	const known = new Set([current?.id, ...queued.map((job) => job.id)]);
	const additions = active.filter((job) => !known.has(job.id));
	return [...queued, ...additions];
}

export function buildActressSyncSummary(
	job: ActressSyncJob,
	tasks: ActressSyncTask[],
): ActressSyncSummary {
	return {
		total: job.total_tasks,
		processed: job.completed,
		updated: job.updated,
		warnings: job.warnings,
		skipped: job.skipped,
		conflicts: job.conflicts,
		failed: job.failed,
		cancelled: job.cancelled,
		active: tasks.filter((task) => task.status === 'running'),
		diagnostics: tasks.filter(
			(task) =>
				['skipped', 'conflict', 'failed', 'cancelled'].includes(task.status) ||
				Boolean(task.warning) ||
				Boolean(task.error_message),
		),
	};
}

export function isActressSyncTerminal(job: ActressSyncJob): boolean {
	return job.status === 'completed' || job.status === 'cancelled';
}
