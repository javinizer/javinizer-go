import type { Accessor, CreateQueryOptions, CreateQueryResult, DefinedCreateQueryResult } from './types.js';
import type { DefaultError, OmitKeyof, QueriesPlaceholderDataFunction, QueryClient, QueryFunction, QueryKey, ThrowOnError } from '@tanstack/query-core';
type CreateQueryOptionsForCreateQueries<TQueryFnData = unknown, TError = DefaultError, TData = TQueryFnData, TQueryKey extends QueryKey = QueryKey> = OmitKeyof<CreateQueryOptions<TQueryFnData, TError, TData, TQueryKey>, 'placeholderData'> & {
    placeholderData?: TQueryFnData | QueriesPlaceholderDataFunction<TQueryFnData>;
};
type MAXIMUM_DEPTH = 20;
type SkipTokenForCreateQueries = symbol;
type GetCreateQueryOptionsForCreateQueries<T> = T extends {
    queryFnData: infer TQueryFnData;
    error?: infer TError;
    data: infer TData;
} ? CreateQueryOptionsForCreateQueries<TQueryFnData, TError, TData> : T extends {
    queryFnData: infer TQueryFnData;
    error?: infer TError;
} ? CreateQueryOptionsForCreateQueries<TQueryFnData, TError> : T extends {
    data: infer TData;
    error?: infer TError;
} ? CreateQueryOptionsForCreateQueries<unknown, TError, TData> : T extends [infer TQueryFnData, infer TError, infer TData] ? CreateQueryOptionsForCreateQueries<TQueryFnData, TError, TData> : T extends [infer TQueryFnData, infer TError] ? CreateQueryOptionsForCreateQueries<TQueryFnData, TError> : T extends [infer TQueryFnData] ? CreateQueryOptionsForCreateQueries<TQueryFnData> : T extends {
    queryFn?: QueryFunction<infer TQueryFnData, infer TQueryKey> | SkipTokenForCreateQueries;
    select?: (data: any) => infer TData;
    throwOnError?: ThrowOnError<any, infer TError, any, any>;
} ? CreateQueryOptionsForCreateQueries<TQueryFnData, unknown extends TError ? DefaultError : TError, unknown extends TData ? TQueryFnData : TData, TQueryKey> : CreateQueryOptionsForCreateQueries;
type GetDefinedOrUndefinedQueryResult<T, TData, TError = unknown> = T extends {
    initialData?: infer TInitialData;
} ? unknown extends TInitialData ? CreateQueryResult<TData, TError> : TInitialData extends TData ? DefinedCreateQueryResult<TData, TError> : TInitialData extends () => infer TInitialDataResult ? unknown extends TInitialDataResult ? CreateQueryResult<TData, TError> : TInitialDataResult extends TData ? DefinedCreateQueryResult<TData, TError> : CreateQueryResult<TData, TError> : CreateQueryResult<TData, TError> : CreateQueryResult<TData, TError>;
type GetCreateQueryResult<T> = T extends {
    queryFnData: any;
    error?: infer TError;
    data: infer TData;
} ? GetDefinedOrUndefinedQueryResult<T, TData, TError> : T extends {
    queryFnData: infer TQueryFnData;
    error?: infer TError;
} ? GetDefinedOrUndefinedQueryResult<T, TQueryFnData, TError> : T extends {
    data: infer TData;
    error?: infer TError;
} ? GetDefinedOrUndefinedQueryResult<T, TData, TError> : T extends [any, infer TError, infer TData] ? GetDefinedOrUndefinedQueryResult<T, TData, TError> : T extends [infer TQueryFnData, infer TError] ? GetDefinedOrUndefinedQueryResult<T, TQueryFnData, TError> : T extends [infer TQueryFnData] ? GetDefinedOrUndefinedQueryResult<T, TQueryFnData> : T extends {
    queryFn?: QueryFunction<infer TQueryFnData, any> | SkipTokenForCreateQueries;
    select?: (data: any) => infer TData;
    throwOnError?: ThrowOnError<any, infer TError, any, any>;
} ? GetDefinedOrUndefinedQueryResult<T, unknown extends TData ? TQueryFnData : TData, unknown extends TError ? DefaultError : TError> : CreateQueryResult;
/**
 * QueriesOptions reducer recursively unwraps function arguments to infer/enforce type param
 */
export type QueriesOptions<T extends Array<any>, TResults extends Array<any> = [], TDepth extends ReadonlyArray<number> = []> = TDepth['length'] extends MAXIMUM_DEPTH ? Array<CreateQueryOptionsForCreateQueries> : T extends [] ? [] : T extends [infer Head] ? [...TResults, GetCreateQueryOptionsForCreateQueries<Head>] : T extends [infer Head, ...infer Tails] ? QueriesOptions<[
    ...Tails
], [
    ...TResults,
    GetCreateQueryOptionsForCreateQueries<Head>
], [
    ...TDepth,
    1
]> : ReadonlyArray<unknown> extends T ? T : T extends Array<CreateQueryOptionsForCreateQueries<infer TQueryFnData, infer TError, infer TData, infer TQueryKey>> ? Array<CreateQueryOptionsForCreateQueries<TQueryFnData, TError, TData, TQueryKey>> : Array<CreateQueryOptionsForCreateQueries>;
/**
 * QueriesResults reducer recursively maps type param to results
 */
export type QueriesResults<T extends Array<any>, TResults extends Array<any> = [], TDepth extends ReadonlyArray<number> = []> = TDepth['length'] extends MAXIMUM_DEPTH ? Array<CreateQueryResult> : T extends [] ? [] : T extends [infer Head] ? [...TResults, GetCreateQueryResult<Head>] : T extends [infer Head, ...infer Tails] ? QueriesResults<[
    ...Tails
], [
    ...TResults,
    GetCreateQueryResult<Head>
], [
    ...TDepth,
    1
]> : {
    [K in keyof T]: GetCreateQueryResult<T[K]>;
};
export declare function createQueries<T extends Array<any>, TCombinedResult = QueriesResults<T>>(createQueriesOptions: Accessor<{
    queries: readonly [...QueriesOptions<T>] | readonly [
        ...{
            [K in keyof T]: GetCreateQueryOptionsForCreateQueries<T[K]>;
        }
    ];
    combine?: (result: QueriesResults<T>) => TCombinedResult;
}>, queryClient?: Accessor<QueryClient>): TCombinedResult;
export {};
//# sourceMappingURL=createQueries.svelte.d.ts.map