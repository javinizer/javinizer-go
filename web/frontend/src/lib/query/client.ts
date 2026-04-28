import { QueryClient } from '@tanstack/svelte-query';
import { browser } from '$app/environment';

let client: QueryClient | undefined = undefined;

export function getQueryClient(): QueryClient {
	if (!browser) {
		return new QueryClient({
			defaultOptions: {
				queries: {
					enabled: false,
				},
			},
		});
	}

	if (!client) {
		client = new QueryClient({
			defaultOptions: {
				queries: {
					enabled: true,
				},
			},
		});
	}

	return client;
}
