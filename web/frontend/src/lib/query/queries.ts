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
