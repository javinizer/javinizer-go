import type { OperationNode } from './operation-node.js';
export interface OutputNode extends OperationNode {
    readonly kind: 'OutputNode';
    readonly selections: ReadonlyArray<OperationNode>;
}
type OutputNodeFactory = Readonly<{
    is(node: OperationNode): node is OutputNode;
    create(selections: ReadonlyArray<OperationNode>): Readonly<OutputNode>;
    cloneWithSelections(output: OutputNode, selections: ReadonlyArray<OperationNode>): Readonly<OutputNode>;
}>;
/**
 * @internal
 */
export declare const OutputNode: OutputNodeFactory;
export {};
