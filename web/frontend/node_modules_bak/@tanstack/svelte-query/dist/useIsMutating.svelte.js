import { useQueryClient } from './useQueryClient.js';
import { ReactiveValue } from './containers.svelte.js';
export function useIsMutating(filters, queryClient) {
    const client = useQueryClient(queryClient);
    const cache = client.getMutationCache();
    return new ReactiveValue(() => client.isMutating(filters), (update) => cache.subscribe(update));
}
