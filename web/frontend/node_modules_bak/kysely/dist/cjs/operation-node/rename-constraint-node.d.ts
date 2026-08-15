import type { OperationNode } from './operation-node.js';
import { IdentifierNode } from './identifier-node.js';
export interface RenameConstraintNode extends OperationNode {
    readonly kind: 'RenameConstraintNode';
    readonly oldName: IdentifierNode;
    readonly newName: IdentifierNode;
}
type RenameConstraintNodeFactory = Readonly<{
    is(node: OperationNode): node is RenameConstraintNode;
    create(oldName: string, newName: string): Readonly<RenameConstraintNode>;
}>;
/**
 * @internal
 */
export declare const RenameConstraintNode: RenameConstraintNodeFactory;
export {};
