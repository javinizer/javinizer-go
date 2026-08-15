import type { OperationNode } from './operation-node.js';
import { IdentifierNode } from './identifier-node.js';
export type DropConstraintNodeProps = Omit<DropConstraintNode, 'kind' | 'constraintName'>;
export interface DropConstraintNode extends OperationNode {
    readonly kind: 'DropConstraintNode';
    readonly constraintName: IdentifierNode;
    readonly ifExists?: boolean;
    readonly modifier?: 'cascade' | 'restrict';
}
type DropConstraintNodeFactory = Readonly<{
    is(node: OperationNode): node is DropConstraintNode;
    create(constraintName: string, params?: DropConstraintNodeProps): Readonly<DropConstraintNode>;
    cloneWith(dropConstraint: DropConstraintNode, props: DropConstraintNodeProps): Readonly<DropConstraintNode>;
}>;
/**
 * @internal
 */
export declare const DropConstraintNode: DropConstraintNodeFactory;
export {};
