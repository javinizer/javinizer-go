import type { OperationNode } from './operation-node.js';
import { IdentifierNode } from './identifier-node.js';
export interface CheckConstraintNode extends OperationNode {
    readonly kind: 'CheckConstraintNode';
    readonly expression: OperationNode;
    readonly name?: IdentifierNode;
}
type CheckConstraintNodeFactory = Readonly<{
    is(node: OperationNode): node is CheckConstraintNode;
    create(expression: OperationNode, constraintName?: string): Readonly<CheckConstraintNode>;
}>;
/**
 * @internal
 */
export declare const CheckConstraintNode: CheckConstraintNodeFactory;
export {};
