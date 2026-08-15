import { type QueryId } from '../util/query-id.js';
import type { RootOperationNode } from './query-compiler.js';
export interface CompiledQuery<O = unknown> {
    readonly query: RootOperationNode;
    readonly queryId: QueryId;
    readonly sql: string;
    readonly parameters: ReadonlyArray<unknown>;
}
type CompiledQueryFactory = Readonly<{
    raw(sql: string, parameters?: unknown[]): Readonly<CompiledQuery>;
}>;
export declare const CompiledQuery: CompiledQueryFactory;
export {};
