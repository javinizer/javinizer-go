import type { DefaultError, InfiniteData, QueryClient, QueryKey } from '@tanstack/query-core';
import type { Accessor, CreateInfiniteQueryOptions, CreateInfiniteQueryResult } from './types.js';
export declare function createInfiniteQuery<TQueryFnData, TError = DefaultError, TData = InfiniteData<TQueryFnData>, TQueryKey extends QueryKey = QueryKey, TPageParam = unknown>(options: Accessor<CreateInfiniteQueryOptions<TQueryFnData, TError, TData, TQueryKey, TPageParam>>, queryClient?: Accessor<QueryClient>): CreateInfiniteQueryResult<TData, TError>;
//# sourceMappingURL=createInfiniteQuery.d.ts.map