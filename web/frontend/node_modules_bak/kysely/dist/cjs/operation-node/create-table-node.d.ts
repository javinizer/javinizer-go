import type { OperationNode } from './operation-node.js';
import type { TableNode } from './table-node.js';
import type { ConstraintNode } from './constraint-node.js';
import type { ColumnDefinitionNode } from './column-definition-node.js';
import type { ArrayItemType } from '../util/type-utils.js';
export declare const ON_COMMIT_ACTIONS: string[];
export type OnCommitAction = ArrayItemType<typeof ON_COMMIT_ACTIONS>;
export type CreateTableNodeParams = Omit<CreateTableNode, 'kind' | 'table' | 'columns' | 'constraints' | 'frontModifiers' | 'endModifiers'>;
export interface CreateTableNode extends OperationNode {
    readonly kind: 'CreateTableNode';
    readonly table: TableNode;
    readonly columns: ReadonlyArray<ColumnDefinitionNode>;
    readonly constraints?: ReadonlyArray<ConstraintNode>;
    readonly temporary?: boolean;
    readonly ifNotExists?: boolean;
    readonly onCommit?: OnCommitAction;
    readonly frontModifiers?: ReadonlyArray<OperationNode>;
    readonly endModifiers?: ReadonlyArray<OperationNode>;
    readonly selectQuery?: OperationNode;
}
type CreateTableNodeFactory = Readonly<{
    is(node: OperationNode): node is CreateTableNode;
    create(table: TableNode): Readonly<CreateTableNode>;
    cloneWithColumn(createTable: CreateTableNode, column: ColumnDefinitionNode): Readonly<CreateTableNode>;
    cloneWithConstraint(createTable: CreateTableNode, constraint: ConstraintNode): Readonly<CreateTableNode>;
    cloneWithFrontModifier(createTable: CreateTableNode, modifier: OperationNode): Readonly<CreateTableNode>;
    cloneWithEndModifier(createTable: CreateTableNode, modifier: OperationNode): Readonly<CreateTableNode>;
    cloneWith(createTable: CreateTableNode, params: CreateTableNodeParams): Readonly<CreateTableNode>;
}>;
/**
 * @internal
 */
export declare const CreateTableNode: CreateTableNodeFactory;
export {};
