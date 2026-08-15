import type { OperationNode } from './operation-node.js';
export interface OnNode extends OperationNode {
    readonly kind: 'OnNode';
    readonly on: OperationNode;
}
type OnNodeFactory = Readonly<{
    is(node: OperationNode): node is OnNode;
    create(filter: OperationNode): Readonly<OnNode>;
    cloneWithOperation(onNode: OnNode, operator: 'And' | 'Or', operation: OperationNode): Readonly<OnNode>;
}>;
/**
 * @internal
 */
export declare const OnNode: OnNodeFactory;
export {};
