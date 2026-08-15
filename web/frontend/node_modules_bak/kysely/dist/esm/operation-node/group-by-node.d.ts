import type { OperationNode } from './operation-node.js';
import type { GroupByItemNode } from './group-by-item-node.js';
export interface GroupByNode extends OperationNode {
    readonly kind: 'GroupByNode';
    readonly items: ReadonlyArray<GroupByItemNode>;
}
type GroupByNodeFactory = Readonly<{
    is(node: OperationNode): node is GroupByNode;
    create(items: ReadonlyArray<GroupByItemNode>): Readonly<GroupByNode>;
    cloneWithItems(groupBy: GroupByNode, items: ReadonlyArray<GroupByItemNode>): Readonly<GroupByNode>;
}>;
/**
 * @internal
 */
export declare const GroupByNode: GroupByNodeFactory;
export {};
