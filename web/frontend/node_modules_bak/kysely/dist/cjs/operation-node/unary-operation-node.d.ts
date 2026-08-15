import type { OperationNode } from './operation-node.js';
export interface UnaryOperationNode extends OperationNode {
    readonly kind: 'UnaryOperationNode';
    readonly operator: OperationNode;
    readonly operand: OperationNode;
}
type UnaryOperationNodeFactory = Readonly<{
    is(node: OperationNode): node is UnaryOperationNode;
    create(operator: OperationNode, operand: OperationNode): Readonly<UnaryOperationNode>;
}>;
/**
 * @internal
 */
export declare const UnaryOperationNode: UnaryOperationNodeFactory;
export {};
