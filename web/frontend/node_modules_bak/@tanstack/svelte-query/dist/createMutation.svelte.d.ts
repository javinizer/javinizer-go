import type { Accessor, CreateMutationOptions, CreateMutationResult } from './types.js';
import type { DefaultError, QueryClient } from '@tanstack/query-core';
/**
 * @param options - A function that returns mutation options
 * @param queryClient - Custom query client which overrides provider
 */
export declare function createMutation<TData = unknown, TError = DefaultError, TVariables = void, TContext = unknown>(options: Accessor<CreateMutationOptions<TData, TError, TVariables, TContext>>, queryClient?: Accessor<QueryClient>): CreateMutationResult<TData, TError, TVariables, TContext>;
//# sourceMappingURL=createMutation.svelte.d.ts.map