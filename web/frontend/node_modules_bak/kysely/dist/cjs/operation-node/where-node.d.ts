import type { OperationNode } from './operation-node.js';
export interface WhereNode extends OperationNode {
    readonly kind: 'WhereNode';
    readonly where: OperationNode;
}
type WhereNodeFactory = Readonly<{
    is(node: OperationNode): node is WhereNode;
    create(filter: OperationNode): Readonly<WhereNode>;
    cloneWithOperation(whereNode: WhereNode, operator: 'And' | 'Or', operation: OperationNode): Readonly<WhereNode>;
}>;
/**
 * @internal
 */
export declare const WhereNode: WhereNodeFactory;
export {};
