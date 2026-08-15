import { AlterTableNode } from '../operation-node/alter-table-node.js';
import type { OperationNodeSource } from '../operation-node/operation-node-source.js';
import type { OnModifyForeignAction } from '../operation-node/references-node.js';
import type { CompiledQuery } from '../query-compiler/compiled-query.js';
import type { QueryExecutor } from '../query-executor/query-executor.js';
import type { Compilable } from '../util/compilable.js';
import type { QueryId } from '../util/query-id.js';
import type { ForeignKeyConstraintBuilder, ForeignKeyConstraintBuilderInterface } from './foreign-key-constraint-builder.js';
export declare class AlterTableAddForeignKeyConstraintBuilder implements ForeignKeyConstraintBuilderInterface<AlterTableAddForeignKeyConstraintBuilder>, OperationNodeSource, Compilable {
    #private;
    constructor(props: AlterTableAddForeignKeyConstraintBuilderProps);
    onDelete(onDelete: OnModifyForeignAction): AlterTableAddForeignKeyConstraintBuilder;
    onUpdate(onUpdate: OnModifyForeignAction): AlterTableAddForeignKeyConstraintBuilder;
    deferrable(): AlterTableAddForeignKeyConstraintBuilder;
    notDeferrable(): AlterTableAddForeignKeyConstraintBuilder;
    initiallyDeferred(): AlterTableAddForeignKeyConstraintBuilder;
    initiallyImmediate(): AlterTableAddForeignKeyConstraintBuilder;
    /**
     * Simply calls the provided function passing `this` as the only argument. `$call` returns
     * what the provided function returns.
     */
    $call<T>(func: (qb: this) => T): T;
    toOperationNode(): AlterTableNode;
    compile(): CompiledQuery;
    execute(): Promise<void>;
}
export interface AlterTableAddForeignKeyConstraintBuilderProps {
    readonly queryId: QueryId;
    readonly executor: QueryExecutor;
    readonly node: AlterTableNode;
    readonly constraintBuilder: ForeignKeyConstraintBuilder;
}
