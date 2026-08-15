import type { Snippet } from 'svelte';
import type { DehydratedState, HydrateOptions, QueryClient } from '@tanstack/query-core';
type Props = {
    children: Snippet;
    state: DehydratedState;
    options: HydrateOptions | undefined;
    queryClient: QueryClient | undefined;
};
declare const HydrationBoundary: import("svelte").Component<Props, {}, "">;
type HydrationBoundary = ReturnType<typeof HydrationBoundary>;
export default HydrationBoundary;
//# sourceMappingURL=HydrationBoundary.svelte.d.ts.map