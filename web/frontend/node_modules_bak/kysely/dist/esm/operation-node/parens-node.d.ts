import type { OperationNode } from './operation-node.js';
export interface ParensNode extends OperationNode {
    readonly kind: 'ParensNode';
    readonly node: OperationNode;
}
type ParensNodeFactory = Readonly<{
    is(node: OperationNode): node is ParensNode;
    create(node: OperationNode): Readonly<ParensNode>;
}>;
/**
 * @internal
 */
export declare const ParensNode: ParensNodeFactory;
export {};
