import type { QueryClient, QueryKey, QueryObserver } from '@tanstack/query-core';
import type { Accessor, CreateBaseQueryOptions, CreateBaseQueryResult } from './types.js';
/**
 * Base implementation for `createQuery` and `createInfiniteQuery`
 * @param options - A function that returns query options
 * @param Observer - The observer from query-core
 * @param queryClient - Custom query client which overrides provider
 */
export declare function createBaseQuery<TQueryFnData, TError, TData, TQueryData, TQueryKey extends QueryKey>(options: Accessor<CreateBaseQueryOptions<TQueryFnData, TError, TData, TQueryData, TQueryKey>>, Observer: typeof QueryObserver, queryClient?: Accessor<QueryClient>): CreateBaseQueryResult<TData, TError>;
//# sourceMappingURL=createBaseQuery.svelte.d.ts.map