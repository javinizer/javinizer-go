import { createQuery } from '@tanstack/svelte-query';
import { apiClient } from '$lib/api/client';

export function createConfigQuery() {
	return createQuery(() => ({
		queryKey: ['config'],
		queryFn: () => apiClient.getConfig(),
		staleTime: 30_000
	}));
}

export function createScrapersQuery() {
	return createQuery(() => ({
		queryKey: ['scrapers'],
		queryFn: () => apiClient.getScrapers(),
		staleTime: 30_000
	}));
}

export function createBatchJobsQuery() {
	return createQuery(() => ({
		queryKey: ['batch-jobs'],
		queryFn: () => apiClient.listBatchJobs(),
		staleTime: 5_000
	}));
}

export function createJobDetailQuery(jobId: string) {
	return createQuery(() => ({
		queryKey: ['job', jobId],
		queryFn: () => apiClient.getJob(jobId),
		staleTime: 5_000
	}));
}

export function createJobOperationsQuery(jobId: string) {
	return createQuery(() => ({
		queryKey: ['job', jobId, 'operations'],
		queryFn: () => apiClient.getJobOperations(jobId),
		staleTime: 5_000
	}));
}

export function createGenreReplacementsQuery(opts?: { limit?: number }) {
	return createQuery(() => ({
		queryKey: ['genre-replacements'],
		queryFn: () => apiClient.listGenreReplacements({ limit: opts?.limit ?? 500 }),
		staleTime: 30_000
	}));
}

export function createWordReplacementsQuery(opts?: { limit?: number }) {
	return createQuery(() => ({
		queryKey: ['word-replacements'],
		queryFn: () => apiClient.listWordReplacements({ limit: opts?.limit ?? 200 }),
		staleTime: 30_000
	}));
}

export function createBatchJobPollingQuery(jobId: string) {
	return createQuery(() => ({
		queryKey: ['batch-job-slim', jobId],
		queryFn: () => apiClient.getBatchJob(jobId),
		refetchInterval: (query) => {
			const status = query.state.data?.status;
			return status === 'completed' || status === 'failed' || status === 'cancelled'
				? false
				: 2000;
		},
		refetchIntervalInBackground: true,
		staleTime: 0
	}));
}
