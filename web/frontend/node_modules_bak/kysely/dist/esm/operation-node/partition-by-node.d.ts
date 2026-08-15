import type { PartitionByItemNode } from './partition-by-item-node.js';
import type { OperationNode } from './operation-node.js';
export interface PartitionByNode extends OperationNode {
    readonly kind: 'PartitionByNode';
    readonly items: ReadonlyArray<PartitionByItemNode>;
}
type PartitionByNodeFactory = Readonly<{
    is(node: OperationNode): node is PartitionByNode;
    create(items: ReadonlyArray<PartitionByItemNode>): Readonly<PartitionByNode>;
    cloneWithItems(partitionBy: PartitionByNode, items: ReadonlyArray<PartitionByItemNode>): Readonly<PartitionByNode>;
}>;
/**
 * @internal
 */
export declare const PartitionByNode: PartitionByNodeFactory;
export {};
