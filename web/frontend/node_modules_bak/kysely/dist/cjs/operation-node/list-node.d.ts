import type { OperationNode } from './operation-node.js';
export interface ListNode extends OperationNode {
    readonly kind: 'ListNode';
    readonly items: ReadonlyArray<OperationNode>;
}
type ListNodeFactory = Readonly<{
    is(node: OperationNode): node is ListNode;
    create(items: ReadonlyArray<OperationNode>): Readonly<ListNode>;
}>;
/**
 * @internal
 */
export declare const ListNode: ListNodeFactory;
export {};
