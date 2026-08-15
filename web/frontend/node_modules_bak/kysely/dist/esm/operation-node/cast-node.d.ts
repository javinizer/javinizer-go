import type { OperationNode } from './operation-node.js';
export interface CastNode extends OperationNode {
    readonly kind: 'CastNode';
    readonly expression: OperationNode;
    readonly dataType: OperationNode;
}
type CastNodeFactory = Readonly<{
    is(node: OperationNode): node is CastNode;
    create(expression: OperationNode, dataType: OperationNode): Readonly<CastNode>;
}>;
/**
 * @internal
 */
export declare const CastNode: CastNodeFactory;
export {};
