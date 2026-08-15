import type { OperationNode } from './operation-node.js';
export interface OrNode extends OperationNode {
    readonly kind: 'OrNode';
    readonly left: OperationNode;
    readonly right: OperationNode;
}
type OrNodeFactory = Readonly<{
    is(node: OperationNode): node is OrNode;
    create(left: OperationNode, right: OperationNode): Readonly<OrNode>;
}>;
/**
 * @internal
 */
export declare const OrNode: OrNodeFactory;
export {};
