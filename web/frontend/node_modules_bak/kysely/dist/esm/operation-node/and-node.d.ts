import type { OperationNode } from './operation-node.js';
export interface AndNode extends OperationNode {
    readonly kind: 'AndNode';
    readonly left: OperationNode;
    readonly right: OperationNode;
}
type AndNodeFactory = Readonly<{
    is(node: OperationNode): node is AndNode;
    create(left: OperationNode, right: OperationNode): Readonly<AndNode>;
}>;
/**
 * @internal
 */
export declare const AndNode: AndNodeFactory;
export {};
