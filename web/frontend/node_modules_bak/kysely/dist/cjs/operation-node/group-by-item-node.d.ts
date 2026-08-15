import type { OperationNode } from './operation-node.js';
export interface GroupByItemNode extends OperationNode {
    readonly kind: 'GroupByItemNode';
    readonly groupBy: OperationNode;
}
type GroupByItemNodeFactory = Readonly<{
    is(node: OperationNode): node is GroupByItemNode;
    create(groupBy: OperationNode): Readonly<GroupByItemNode>;
}>;
/**
 * @internal
 */
export declare const GroupByItemNode: GroupByItemNodeFactory;
export {};
