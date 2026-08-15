import type { OperationNode } from './operation-node.js';
export interface IdentifierNode extends OperationNode {
    readonly kind: 'IdentifierNode';
    readonly name: string;
}
type IdentifierNodeFactory = Readonly<{
    is(node: OperationNode): node is IdentifierNode;
    create(name: string): Readonly<IdentifierNode>;
}>;
/**
 * @internal
 */
export declare const IdentifierNode: IdentifierNodeFactory;
export {};
