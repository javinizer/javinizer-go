import type { OperationNode } from './operation-node.js';
export interface OrActionNode extends OperationNode {
    readonly kind: 'OrActionNode';
    readonly action: string;
}
type OrActionNodeFactory = Readonly<{
    is(node: OperationNode): node is OrActionNode;
    create(action: string): Readonly<OrActionNode>;
}>;
/**
 * @internal
 */
export declare const OrActionNode: OrActionNodeFactory;
export {};
