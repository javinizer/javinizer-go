import type { OperationNode } from './operation-node.js';
export interface FromNode extends OperationNode {
    readonly kind: 'FromNode';
    readonly froms: ReadonlyArray<OperationNode>;
}
type FromNodeFactory = Readonly<{
    is(node: OperationNode): node is FromNode;
    create(froms: ReadonlyArray<OperationNode>): Readonly<FromNode>;
    cloneWithFroms(from: FromNode, froms: ReadonlyArray<OperationNode>): Readonly<FromNode>;
}>;
/**
 * @internal
 */
export declare const FromNode: FromNodeFactory;
export {};
