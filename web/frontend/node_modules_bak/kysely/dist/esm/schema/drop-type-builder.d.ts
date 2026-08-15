import { DropTypeNode } from '../operation-node/drop-type-node.js';
import type { OperationNodeSource } from '../operation-node/operation-node-source.js';
import type { CompiledQuery } from '../query-compiler/compiled-query.js';
import type { Compilable } from '../util/compilable.js';
import type { QueryExecutor } from '../query-executor/query-executor.js';
import type { QueryId } from '../util/query-id.js';
export declare class DropTypeBuilder implements OperationNodeSource, Compilable {
    #private;
    constructor(props: DropTypeBuilderProps);
    ifExists(): DropTypeBuilder;
    /**
     * Simply calls the provided function passing `this` as the only argument. `$call` returns
     * what the provided function returns.
     */
    $call<T>(func: (qb: this) => T): T;
    toOperationNode(): DropTypeNode;
    compile(): CompiledQuery;
    execute(): Promise<void>;
}
export interface DropTypeBuilderProps {
    readonly queryId: QueryId;
    readonly executor: QueryExecutor;
    readonly node: DropTypeNode;
}
