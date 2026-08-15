import type { ColumnDefinitionNode } from './column-definition-node.js';
import type { OperationNode } from './operation-node.js';
export interface AddColumnNode extends OperationNode {
    readonly kind: 'AddColumnNode';
    readonly column: ColumnDefinitionNode;
}
type AddColumnNodeFactory = Readonly<{
    is(node: OperationNode): node is AddColumnNode;
    create(column: ColumnDefinitionNode): Readonly<AddColumnNode>;
}>;
/**
 * @internal
 */
export declare const AddColumnNode: AddColumnNodeFactory;
export {};
