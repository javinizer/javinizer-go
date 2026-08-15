import type { OperationNode } from './operation-node.js';
import { ColumnNode } from './column-node.js';
export interface RenameColumnNode extends OperationNode {
    readonly kind: 'RenameColumnNode';
    readonly column: ColumnNode;
    readonly renameTo: ColumnNode;
}
type RenameColumnNodeFactory = Readonly<{
    is(node: OperationNode): node is RenameColumnNode;
    create(column: string, newColumn: string): Readonly<RenameColumnNode>;
}>;
/**
 * @internal
 */
export declare const RenameColumnNode: RenameColumnNodeFactory;
export {};
