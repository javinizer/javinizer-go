import type { OperationNode } from './operation-node.js';
export interface ValueListNode extends OperationNode {
    readonly kind: 'ValueListNode';
    readonly values: ReadonlyArray<OperationNode>;
}
type ValueListNodeFactory = Readonly<{
    is(node: OperationNode): node is ValueListNode;
    create(values: ReadonlyArray<OperationNode>): Readonly<ValueListNode>;
}>;
/**
 * @internal
 */
export declare const ValueListNode: ValueListNodeFactory;
export {};
