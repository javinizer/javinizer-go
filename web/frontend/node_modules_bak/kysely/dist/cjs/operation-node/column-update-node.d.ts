import type { OperationNode } from './operation-node.js';
export interface ColumnUpdateNode extends OperationNode {
    readonly kind: 'ColumnUpdateNode';
    readonly column: OperationNode;
    readonly value: OperationNode;
}
type ColumnUpdateNodeFactory = Readonly<{
    is(node: OperationNode): node is ColumnUpdateNode;
    create(column: OperationNode, value: OperationNode): Readonly<ColumnUpdateNode>;
}>;
/**
 * @internal
 */
export declare const ColumnUpdateNode: ColumnUpdateNodeFactory;
export {};
