import type { OperationNode } from './operation-node.js';
export interface WhenNode extends OperationNode {
    readonly kind: 'WhenNode';
    readonly condition: OperationNode;
    readonly result?: OperationNode;
}
type WhenNodeFactory = Readonly<{
    is(node: OperationNode): node is WhenNode;
    create(condition: OperationNode): Readonly<WhenNode>;
    cloneWithResult(whenNode: WhenNode, result: OperationNode): Readonly<WhenNode>;
}>;
/**
 * @internal
 */
export declare const WhenNode: WhenNodeFactory;
export {};
