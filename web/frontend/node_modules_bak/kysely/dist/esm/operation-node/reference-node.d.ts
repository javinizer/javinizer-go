import type { OperationNode } from './operation-node.js';
import type { ColumnNode } from './column-node.js';
import type { TableNode } from './table-node.js';
import { SelectAllNode } from './select-all-node.js';
export interface ReferenceNode extends OperationNode {
    readonly kind: 'ReferenceNode';
    readonly column: ColumnNode | SelectAllNode;
    readonly table?: TableNode;
}
type ReferenceNodeFactory = Readonly<{
    is(node: OperationNode): node is ReferenceNode;
    create(column: ColumnNode, table?: TableNode): Readonly<ReferenceNode>;
    createSelectAll(table: TableNode): Readonly<ReferenceNode>;
}>;
/**
 * @internal
 */
export declare const ReferenceNode: ReferenceNodeFactory;
export {};
