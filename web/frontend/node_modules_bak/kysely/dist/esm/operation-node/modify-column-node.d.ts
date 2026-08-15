import type { ColumnDefinitionNode } from './column-definition-node.js';
import type { OperationNode } from './operation-node.js';
export interface ModifyColumnNode extends OperationNode {
    readonly kind: 'ModifyColumnNode';
    readonly column: ColumnDefinitionNode;
}
type ModifyColumnNodeFactory = Readonly<{
    is(node: OperationNode): node is ModifyColumnNode;
    create(column: ColumnDefinitionNode): Readonly<ModifyColumnNode>;
}>;
/**
 * @internal
 */
export declare const ModifyColumnNode: ModifyColumnNodeFactory;
export {};
