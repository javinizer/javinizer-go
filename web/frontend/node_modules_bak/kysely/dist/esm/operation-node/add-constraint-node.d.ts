import type { OperationNode } from './operation-node.js';
import type { ConstraintNode } from './constraint-node.js';
export interface AddConstraintNode extends OperationNode {
    readonly kind: 'AddConstraintNode';
    readonly constraint: ConstraintNode;
}
type AddConstraintNodeFactory = Readonly<{
    is(node: OperationNode): node is AddConstraintNode;
    create(constraint: ConstraintNode): Readonly<AddConstraintNode>;
}>;
/**
 * @internal
 */
export declare const AddConstraintNode: AddConstraintNodeFactory;
export {};
