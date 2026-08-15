import { DropSchemaNode } from '../operation-node/drop-schema-node.js';
import type { OperationNodeSource } from '../operation-node/operation-node-source.js';
import type { CompiledQuery } from '../query-compiler/compiled-query.js';
import type { Compilable } from '../util/compilable.js';
import type { QueryExecutor } from '../query-executor/query-executor.js';
import type { QueryId } from '../util/query-id.js';
export declare class DropSchemaBuilder implements OperationNodeSource, Compilable {
    #private;
    constructor(props: DropSchemaBuilderProps);
    ifExists(): DropSchemaBuilder;
    cascade(): DropSchemaBuilder;
    /**
     * Simply calls the provided function passing `this` as the only argument. `$call` returns
     * what the provided function returns.
     */
    $call<T>(func: (qb: this) => T): T;
    toOperationNode(): DropSchemaNode;
    compile(): CompiledQuery;
    execute(): Promise<void>;
}
export interface DropSchemaBuilderProps {
    readonly queryId: QueryId;
    readonly executor: QueryExecutor;
    readonly node: DropSchemaNode;
}
