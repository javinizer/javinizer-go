import { CreateSchemaNode } from '../operation-node/create-schema-node.js';
import type { OperationNodeSource } from '../operation-node/operation-node-source.js';
import type { CompiledQuery } from '../query-compiler/compiled-query.js';
import type { Compilable } from '../util/compilable.js';
import type { QueryExecutor } from '../query-executor/query-executor.js';
import type { QueryId } from '../util/query-id.js';
export declare class CreateSchemaBuilder implements OperationNodeSource, Compilable {
    #private;
    constructor(props: CreateSchemaBuilderProps);
    ifNotExists(): CreateSchemaBuilder;
    /**
     * Simply calls the provided function passing `this` as the only argument. `$call` returns
     * what the provided function returns.
     */
    $call<T>(func: (qb: this) => T): T;
    toOperationNode(): CreateSchemaNode;
    compile(): CompiledQuery;
    execute(): Promise<void>;
}
export interface CreateSchemaBuilderProps {
    readonly queryId: QueryId;
    readonly executor: QueryExecutor;
    readonly node: CreateSchemaNode;
}
