import type { AlterTableNode } from '../operation-node/alter-table-node.js';
import type { OperationNodeSource } from '../operation-node/operation-node-source.js';
import type { CompiledQuery } from '../query-compiler/compiled-query.js';
import type { QueryExecutor } from '../query-executor/query-executor.js';
import type { Compilable } from '../util/compilable.js';
import type { QueryId } from '../util/query-id.js';
export declare class AlterTableExecutor implements OperationNodeSource, Compilable {
    #private;
    constructor(props: AlterTableExecutorProps);
    toOperationNode(): AlterTableNode;
    compile(): CompiledQuery;
    execute(): Promise<void>;
}
export interface AlterTableExecutorProps {
    readonly queryId: QueryId;
    readonly executor: QueryExecutor;
    readonly node: AlterTableNode;
}
