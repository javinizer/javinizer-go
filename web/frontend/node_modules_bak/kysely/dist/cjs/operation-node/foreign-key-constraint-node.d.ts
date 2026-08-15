import type { ColumnNode } from './column-node.js';
import { IdentifierNode } from './identifier-node.js';
import type { OperationNode } from './operation-node.js';
import { type OnModifyForeignAction, ReferencesNode } from './references-node.js';
import type { TableNode } from './table-node.js';
export type ForeignKeyConstraintNodeProps = Omit<ForeignKeyConstraintNode, 'kind' | 'columns' | 'references'>;
export interface ForeignKeyConstraintNode extends OperationNode {
    readonly kind: 'ForeignKeyConstraintNode';
    readonly columns: ReadonlyArray<ColumnNode>;
    readonly references: ReferencesNode;
    readonly onDelete?: OnModifyForeignAction;
    readonly onUpdate?: OnModifyForeignAction;
    readonly name?: IdentifierNode;
    readonly deferrable?: boolean;
    readonly initiallyDeferred?: boolean;
}
type ForeignKeyConstraintNodeFactory = Readonly<{
    is(node: OperationNode): node is ForeignKeyConstraintNode;
    create(sourceColumns: ReadonlyArray<ColumnNode>, targetTable: TableNode, targetColumns: ReadonlyArray<ColumnNode>, constraintName?: string): Readonly<ForeignKeyConstraintNode>;
    cloneWith(node: ForeignKeyConstraintNode, props: ForeignKeyConstraintNodeProps): Readonly<ForeignKeyConstraintNode>;
}>;
/**
 * @internal
 */
export declare const ForeignKeyConstraintNode: ForeignKeyConstraintNodeFactory;
export {};
