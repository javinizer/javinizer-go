import type { OperationNode } from './operation-node.js';
export interface LimitNode extends OperationNode {
    readonly kind: 'LimitNode';
    readonly limit: OperationNode;
}
type LimitNodeFactory = Readonly<{
    is(node: OperationNode): node is LimitNode;
    create(limit: OperationNode): Readonly<LimitNode>;
}>;
/**
 * @internal
 */
export declare const LimitNode: LimitNodeFactory;
export {};
