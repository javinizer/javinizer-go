import { ReactiveValue } from './containers.svelte.js';
import { useQueryClient } from './useQueryClient.js';
export function useIsFetching(filters, queryClient) {
    const client = useQueryClient(queryClient);
    const queryCache = client.getQueryCache();
    return new ReactiveValue(() => client.isFetching(filters), (update) => queryCache.subscribe(update));
}
