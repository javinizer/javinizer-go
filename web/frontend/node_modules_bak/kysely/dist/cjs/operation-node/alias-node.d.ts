import type { OperationNode } from './operation-node.js';
export interface AliasNode extends OperationNode {
    readonly kind: 'AliasNode';
    readonly node: OperationNode;
    readonly alias: OperationNode;
}
type AliasNodeFactory = Readonly<{
    is(node: OperationNode): node is AliasNode;
    create(node: OperationNode, alias: OperationNode): Readonly<AliasNode>;
}>;
/**
 * @internal
 */
export declare const AliasNode: AliasNodeFactory;
export {};
