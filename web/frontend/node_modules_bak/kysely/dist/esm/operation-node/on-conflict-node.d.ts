import type { ColumnNode } from './column-node.js';
import type { ColumnUpdateNode } from './column-update-node.js';
import type { IdentifierNode } from './identifier-node.js';
import type { OperationNode } from './operation-node.js';
import { WhereNode } from './where-node.js';
export type OnConflictNodeProps = Omit<OnConflictNode, 'kind' | 'indexWhere' | 'updateWhere'>;
export interface OnConflictNode extends OperationNode {
    readonly kind: 'OnConflictNode';
    readonly columns?: ReadonlyArray<ColumnNode>;
    readonly constraint?: IdentifierNode;
    readonly indexExpression?: OperationNode;
    readonly indexWhere?: WhereNode;
    readonly updates?: ReadonlyArray<ColumnUpdateNode>;
    readonly updateWhere?: WhereNode;
    readonly doNothing?: boolean;
}
type OnConflictNodeFactory = Readonly<{
    is(node: OperationNode): node is OnConflictNode;
    create(): Readonly<OnConflictNode>;
    cloneWith(node: OnConflictNode, props: OnConflictNodeProps): Readonly<OnConflictNode>;
    cloneWithIndexWhere(node: OnConflictNode, operation: OperationNode): Readonly<OnConflictNode>;
    cloneWithIndexOrWhere(node: OnConflictNode, operation: OperationNode): Readonly<OnConflictNode>;
    cloneWithUpdateWhere(node: OnConflictNode, operation: OperationNode): Readonly<OnConflictNode>;
    cloneWithUpdateOrWhere(node: OnConflictNode, operation: OperationNode): Readonly<OnConflictNode>;
    cloneWithoutIndexWhere(node: OnConflictNode): Readonly<OnConflictNode>;
    cloneWithoutUpdateWhere(node: OnConflictNode): Readonly<OnConflictNode>;
}>;
/**
 * @internal
 */
export declare const OnConflictNode: OnConflictNodeFactory;
export {};
