import { QueryObserver } from '@tanstack/query-core';
import { createBaseQuery } from './createBaseQuery.svelte.js';
export function createQuery(options, queryClient) {
    return createBaseQuery(options, QueryObserver, queryClient);
}
