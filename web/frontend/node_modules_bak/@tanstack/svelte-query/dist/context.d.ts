import type { QueryClient } from '@tanstack/query-core';
import type { Box } from './containers.svelte';
/** Retrieves a Client from Svelte's context */
export declare const getQueryClientContext: () => QueryClient;
/** Sets a QueryClient on Svelte's context */
export declare const setQueryClientContext: (client: QueryClient) => void;
/** Retrieves a `isRestoring` from Svelte's context */
export declare const getIsRestoringContext: () => Box<boolean>;
/** Sets a `isRestoring` on Svelte's context */
export declare const setIsRestoringContext: (isRestoring: Box<boolean>) => void;
//# sourceMappingURL=context.d.ts.map