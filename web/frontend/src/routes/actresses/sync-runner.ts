import type { ActressSyncJob, ActressSyncTask } from '$lib/api/types';

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
