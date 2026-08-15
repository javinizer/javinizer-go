import type { AliasNode } from './alias-node.js';
import type { OperationNode } from './operation-node.js';
import { SelectAllNode } from './select-all-node.js';
import type { SimpleReferenceExpressionNode } from './simple-reference-expression-node.js';
import type { TableNode } from './table-node.js';
type SelectionNodeChild = SimpleReferenceExpressionNode | AliasNode | SelectAllNode;
export interface SelectionNode extends OperationNode {
    readonly kind: 'SelectionNode';
    readonly selection: SelectionNodeChild;
}
type SelectionNodeFactory = Readonly<{
    is(node: OperationNode): node is SelectionNode;
    create(selection: SelectionNodeChild): Readonly<SelectionNode>;
    createSelectAll(): Readonly<SelectionNode>;
    createSelectAllFromTable(table: TableNode): Readonly<SelectionNode>;
}>;
/**
 * @internal
 */
export declare const SelectionNode: SelectionNodeFactory;
export {};
