import type { OperationNode } from './operation-node.js';
export type SetOperator = 'union' | 'intersect' | 'except';
export interface SetOperationNode extends OperationNode {
    kind: 'SetOperationNode';
    operator: SetOperator;
    expression: OperationNode;
    all: boolean;
}
type SetOperationNodeFactory = Readonly<{
    is(node: OperationNode): node is SetOperationNode;
    create(operator: SetOperator, expression: OperationNode, all: boolean): Readonly<SetOperationNode>;
}>;
/**
 * @internal
 */
export declare const SetOperationNode: SetOperationNodeFactory;
export {};
