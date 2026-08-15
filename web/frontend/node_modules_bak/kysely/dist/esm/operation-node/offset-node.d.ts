import type { OperationNode } from './operation-node.js';
export interface OffsetNode extends OperationNode {
    readonly kind: 'OffsetNode';
    readonly offset: OperationNode;
}
type OffsetNodeFactory = Readonly<{
    is(node: OperationNode): node is OffsetNode;
    create(offset: OperationNode): Readonly<OffsetNode>;
}>;
/**
 * @internal
 */
export declare const OffsetNode: OffsetNodeFactory;
export {};
