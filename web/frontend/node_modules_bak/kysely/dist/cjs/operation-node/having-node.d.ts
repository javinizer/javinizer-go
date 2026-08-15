import type { OperationNode } from './operation-node.js';
export interface HavingNode extends OperationNode {
    readonly kind: 'HavingNode';
    readonly having: OperationNode;
}
type HavingNodeFactory = Readonly<{
    is(node: OperationNode): node is HavingNode;
    create(filter: OperationNode): Readonly<HavingNode>;
    cloneWithOperation(havingNode: HavingNode, operator: 'And' | 'Or', operation: OperationNode): Readonly<HavingNode>;
}>;
/**
 * @internal
 */
export declare const HavingNode: HavingNodeFactory;
export {};
