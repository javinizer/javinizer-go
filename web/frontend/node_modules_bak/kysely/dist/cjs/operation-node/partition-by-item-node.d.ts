import type { OperationNode } from './operation-node.js';
import type { SimpleReferenceExpressionNode } from './simple-reference-expression-node.js';
export interface PartitionByItemNode extends OperationNode {
    readonly kind: 'PartitionByItemNode';
    readonly partitionBy: SimpleReferenceExpressionNode;
}
type PartitionByItemNodeFactory = Readonly<{
    is(node: OperationNode): node is PartitionByItemNode;
    create(partitionBy: SimpleReferenceExpressionNode): Readonly<PartitionByItemNode>;
}>;
/**
 * @internal
 */
export declare const PartitionByItemNode: PartitionByItemNodeFactory;
export {};
