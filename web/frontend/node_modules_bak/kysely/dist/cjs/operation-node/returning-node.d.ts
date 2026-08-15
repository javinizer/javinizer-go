import type { OperationNode } from './operation-node.js';
import type { SelectionNode } from './selection-node.js';
export interface ReturningNode extends OperationNode {
    readonly kind: 'ReturningNode';
    readonly selections: ReadonlyArray<SelectionNode>;
}
type ReturningNodeFactory = Readonly<{
    is(node: OperationNode): node is ReturningNode;
    create(selections: ReadonlyArray<SelectionNode>): Readonly<ReturningNode>;
    cloneWithSelections(returning: ReturningNode, selections: ReadonlyArray<SelectionNode>): Readonly<ReturningNode>;
}>;
/**
 * @internal
 */
export declare const ReturningNode: ReturningNodeFactory;
export {};
