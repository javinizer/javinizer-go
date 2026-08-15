import { OnNode } from './on-node.js';
import type { OperationNode } from './operation-node.js';
export type JoinType = 'InnerJoin' | 'LeftJoin' | 'RightJoin' | 'FullJoin' | 'CrossJoin' | 'LateralInnerJoin' | 'LateralLeftJoin' | 'LateralCrossJoin' | 'Using' | 'OuterApply' | 'CrossApply';
export interface JoinNode extends OperationNode {
    readonly kind: 'JoinNode';
    readonly joinType: JoinType;
    readonly table: OperationNode;
    readonly on?: OnNode;
}
type JoinNodeFactory = Readonly<{
    is(node: OperationNode): node is JoinNode;
    create(joinType: JoinType, table: OperationNode): Readonly<JoinNode>;
    createWithOn(joinType: JoinType, table: OperationNode, on: OperationNode): Readonly<JoinNode>;
    cloneWithOn(joinNode: JoinNode, operation: OperationNode): Readonly<JoinNode>;
}>;
/**
 * @internal
 */
export declare const JoinNode: JoinNodeFactory;
export {};
