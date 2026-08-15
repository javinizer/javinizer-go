import type { OperationNode } from './operation-node.js';
export interface BinaryOperationNode extends OperationNode {
    readonly kind: 'BinaryOperationNode';
    readonly leftOperand: OperationNode;
    readonly operator: OperationNode;
    readonly rightOperand: OperationNode;
}
type BinaryOperationNodeFactory = Readonly<{
    is(node: OperationNode): node is BinaryOperationNode;
    create(leftOperand: OperationNode, operator: OperationNode, rightOperand: OperationNode): Readonly<BinaryOperationNode>;
}>;
/**
 * @internal
 */
export declare const BinaryOperationNode: BinaryOperationNodeFactory;
export {};
