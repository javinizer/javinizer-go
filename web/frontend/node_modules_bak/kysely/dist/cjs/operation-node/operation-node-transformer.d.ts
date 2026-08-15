import type { AliasNode } from './alias-node.js';
import type { ColumnNode } from './column-node.js';
import type { IdentifierNode } from './identifier-node.js';
import type { OperationNode } from './operation-node.js';
import type { ReferenceNode } from './reference-node.js';
import type { SelectAllNode } from './select-all-node.js';
import type { SelectionNode } from './selection-node.js';
import type { TableNode } from './table-node.js';
import type { AndNode } from './and-node.js';
import type { JoinNode } from './join-node.js';
import type { OrNode } from './or-node.js';
import type { ParensNode } from './parens-node.js';
import type { PrimitiveValueListNode } from './primitive-value-list-node.js';
import type { RawNode } from './raw-node.js';
import type { SelectQueryNode } from './select-query-node.js';
import type { ValueListNode } from './value-list-node.js';
import type { ValueNode } from './value-node.js';
import type { OperatorNode } from './operator-node.js';
import type { FromNode } from './from-node.js';
import type { WhereNode } from './where-node.js';
import type { InsertQueryNode } from './insert-query-node.js';
import type { DeleteQueryNode } from './delete-query-node.js';
import type { ReturningNode } from './returning-node.js';
import type { CreateTableNode } from './create-table-node.js';
import type { AddColumnNode } from './add-column-node.js';
import type { DropTableNode } from './drop-table-node.js';
import type { DataTypeNode } from './data-type-node.js';
import type { OrderByNode } from './order-by-node.js';
import type { OrderByItemNode } from './order-by-item-node.js';
import type { GroupByNode } from './group-by-node.js';
import type { GroupByItemNode } from './group-by-item-node.js';
import type { UpdateQueryNode } from './update-query-node.js';
import type { ColumnUpdateNode } from './column-update-node.js';
import type { LimitNode } from './limit-node.js';
import type { OffsetNode } from './offset-node.js';
import type { OnConflictNode } from './on-conflict-node.js';
import type { CreateIndexNode } from './create-index-node.js';
import type { ListNode } from './list-node.js';
import type { DropIndexNode } from './drop-index-node.js';
import type { PrimaryKeyConstraintNode } from './primary-key-constraint-node.js';
import type { UniqueConstraintNode } from './unique-constraint-node.js';
import type { ReferencesNode } from './references-node.js';
import type { CheckConstraintNode } from './check-constraint-node.js';
import type { WithNode } from './with-node.js';
import type { CommonTableExpressionNode } from './common-table-expression-node.js';
import type { CommonTableExpressionNameNode } from './common-table-expression-name-node.js';
import type { HavingNode } from './having-node.js';
import type { CreateSchemaNode } from './create-schema-node.js';
import type { DropSchemaNode } from './drop-schema-node.js';
import type { AlterTableNode } from './alter-table-node.js';
import type { DropColumnNode } from './drop-column-node.js';
import type { RenameColumnNode } from './rename-column-node.js';
import type { AlterColumnNode } from './alter-column-node.js';
import type { AddConstraintNode } from './add-constraint-node.js';
import type { DropConstraintNode } from './drop-constraint-node.js';
import type { ForeignKeyConstraintNode } from './foreign-key-constraint-node.js';
import type { ColumnDefinitionNode } from './column-definition-node.js';
import type { ModifyColumnNode } from './modify-column-node.js';
import type { OnDuplicateKeyNode } from './on-duplicate-key-node.js';
import type { CreateViewNode } from './create-view-node.js';
import type { DropViewNode } from './drop-view-node.js';
import type { GeneratedNode } from './generated-node.js';
import type { DefaultValueNode } from './default-value-node.js';
import type { OnNode } from './on-node.js';
import type { ValuesNode } from './values-node.js';
import type { SelectModifierNode } from './select-modifier-node.js';
import type { CreateTypeNode } from './create-type-node.js';
import type { DropTypeNode } from './drop-type-node.js';
import type { ExplainNode } from './explain-node.js';
import type { SchemableIdentifierNode } from './schemable-identifier-node.js';
import type { DefaultInsertValueNode } from './default-insert-value-node.js';
import type { AggregateFunctionNode } from './aggregate-function-node.js';
import type { OverNode } from './over-node.js';
import type { PartitionByNode } from './partition-by-node.js';
import type { PartitionByItemNode } from './partition-by-item-node.js';
import type { SetOperationNode } from './set-operation-node.js';
import type { BinaryOperationNode } from './binary-operation-node.js';
import type { UnaryOperationNode } from './unary-operation-node.js';
import type { UsingNode } from './using-node.js';
import type { FunctionNode } from './function-node.js';
import type { CaseNode } from './case-node.js';
import type { WhenNode } from './when-node.js';
import type { JSONReferenceNode } from './json-reference-node.js';
import type { JSONPathNode } from './json-path-node.js';
import type { JSONPathLegNode } from './json-path-leg-node.js';
import type { JSONOperatorChainNode } from './json-operator-chain-node.js';
import type { TupleNode } from './tuple-node.js';
import type { MergeQueryNode } from './merge-query-node.js';
import type { MatchedNode } from './matched-node.js';
import type { AddIndexNode } from './add-index-node.js';
import type { CastNode } from './cast-node.js';
import type { FetchNode } from './fetch-node.js';
import type { TopNode } from './top-node.js';
import type { OutputNode } from './output-node.js';
import type { RefreshMaterializedViewNode } from './refresh-materialized-view-node.js';
import type { OrActionNode } from './or-action-node.js';
import type { CollateNode } from './collate-node.js';
import type { QueryId } from '../util/query-id.js';
import type { RenameConstraintNode } from './rename-constraint-node.js';
/**
 * Transforms an operation node tree into another one.
 *
 * Kysely queries are expressed internally as a tree of objects (operation nodes).
 * `OperationNodeTransformer` takes such a tree as its input and returns a
 * transformed deep copy of it. By default the `OperationNodeTransformer`
 * does nothing. You need to override one or more methods to make it do
 * something.
 *
 * There's a method for each node type. For example if you'd like to convert
 * each identifier (table name, column name, alias etc.) from camelCase to
 * snake_case, you'd do something like this:
 *
 * ```ts
 * import { type IdentifierNode, OperationNodeTransformer } from 'kysely'
 * import snakeCase from 'lodash/snakeCase'
 *
 * class CamelCaseTransformer extends OperationNodeTransformer {
 *   override transformIdentifier(node: IdentifierNode): IdentifierNode {
 *     node = super.transformIdentifier(node)
 *
 *     return {
 *       ...node,
 *       name: snakeCase(node.name),
 *     }
 *   }
 * }
 *
 * const transformer = new CamelCaseTransformer()
 *
 * const query = db.selectFrom('person').select(['first_name', 'last_name'])
 *
 * const tree = transformer.transformNode(query.toOperationNode())
 * ```
 */
export declare class OperationNodeTransformer {
    #private;
    protected readonly nodeStack: OperationNode[];
    transformNode<T extends OperationNode | undefined>(node: T, queryId?: QueryId): T;
    protected transformNodeImpl<T extends OperationNode>(node: T, queryId?: QueryId): T;
    protected transformNodeList<T extends ReadonlyArray<OperationNode> | undefined>(list: T, queryId?: QueryId): T;
    protected transformSelectQuery(node: SelectQueryNode, queryId?: QueryId): SelectQueryNode;
    protected transformSelection(node: SelectionNode, queryId?: QueryId): SelectionNode;
    protected transformColumn(node: ColumnNode, queryId?: QueryId): ColumnNode;
    protected transformAlias(node: AliasNode, queryId?: QueryId): AliasNode;
    protected transformTable(node: TableNode, queryId?: QueryId): TableNode;
    protected transformFrom(node: FromNode, queryId?: QueryId): FromNode;
    protected transformReference(node: ReferenceNode, queryId?: QueryId): ReferenceNode;
    protected transformAnd(node: AndNode, queryId?: QueryId): AndNode;
    protected transformOr(node: OrNode, queryId?: QueryId): OrNode;
    protected transformValueList(node: ValueListNode, queryId?: QueryId): ValueListNode;
    protected transformParens(node: ParensNode, queryId?: QueryId): ParensNode;
    protected transformJoin(node: JoinNode, queryId?: QueryId): JoinNode;
    protected transformRaw(node: RawNode, queryId?: QueryId): RawNode;
    protected transformWhere(node: WhereNode, queryId?: QueryId): WhereNode;
    protected transformInsertQuery(node: InsertQueryNode, queryId?: QueryId): InsertQueryNode;
    protected transformValues(node: ValuesNode, queryId?: QueryId): ValuesNode;
    protected transformDeleteQuery(node: DeleteQueryNode, queryId?: QueryId): DeleteQueryNode;
    protected transformReturning(node: ReturningNode, queryId?: QueryId): ReturningNode;
    protected transformCreateTable(node: CreateTableNode, queryId?: QueryId): CreateTableNode;
    protected transformColumnDefinition(node: ColumnDefinitionNode, queryId?: QueryId): ColumnDefinitionNode;
    protected transformAddColumn(node: AddColumnNode, queryId?: QueryId): AddColumnNode;
    protected transformDropTable(node: DropTableNode, queryId?: QueryId): DropTableNode;
    protected transformOrderBy(node: OrderByNode, queryId?: QueryId): OrderByNode;
    protected transformOrderByItem(node: OrderByItemNode, queryId?: QueryId): OrderByItemNode;
    protected transformGroupBy(node: GroupByNode, queryId?: QueryId): GroupByNode;
    protected transformGroupByItem(node: GroupByItemNode, queryId?: QueryId): GroupByItemNode;
    protected transformUpdateQuery(node: UpdateQueryNode, queryId?: QueryId): UpdateQueryNode;
    protected transformColumnUpdate(node: ColumnUpdateNode, queryId?: QueryId): ColumnUpdateNode;
    protected transformLimit(node: LimitNode, queryId?: QueryId): LimitNode;
    protected transformOffset(node: OffsetNode, queryId?: QueryId): OffsetNode;
    protected transformOnConflict(node: OnConflictNode, queryId?: QueryId): OnConflictNode;
    protected transformOnDuplicateKey(node: OnDuplicateKeyNode, queryId?: QueryId): OnDuplicateKeyNode;
    protected transformCreateIndex(node: CreateIndexNode, queryId?: QueryId): CreateIndexNode;
    protected transformList(node: ListNode, queryId?: QueryId): ListNode;
    protected transformDropIndex(node: DropIndexNode, queryId?: QueryId): DropIndexNode;
    protected transformPrimaryKeyConstraint(node: PrimaryKeyConstraintNode, queryId?: QueryId): PrimaryKeyConstraintNode;
    protected transformUniqueConstraint(node: UniqueConstraintNode, queryId?: QueryId): UniqueConstraintNode;
    protected transformForeignKeyConstraint(node: ForeignKeyConstraintNode, queryId?: QueryId): ForeignKeyConstraintNode;
    protected transformSetOperation(node: SetOperationNode, queryId?: QueryId): SetOperationNode;
    protected transformReferences(node: ReferencesNode, queryId?: QueryId): ReferencesNode;
    protected transformCheckConstraint(node: CheckConstraintNode, queryId?: QueryId): CheckConstraintNode;
    protected transformWith(node: WithNode, queryId?: QueryId): WithNode;
    protected transformCommonTableExpression(node: CommonTableExpressionNode, queryId?: QueryId): CommonTableExpressionNode;
    protected transformCommonTableExpressionName(node: CommonTableExpressionNameNode, queryId?: QueryId): CommonTableExpressionNameNode;
    protected transformHaving(node: HavingNode, queryId?: QueryId): HavingNode;
    protected transformCreateSchema(node: CreateSchemaNode, queryId?: QueryId): CreateSchemaNode;
    protected transformDropSchema(node: DropSchemaNode, queryId?: QueryId): DropSchemaNode;
    protected transformAlterTable(node: AlterTableNode, queryId?: QueryId): AlterTableNode;
    protected transformDropColumn(node: DropColumnNode, queryId?: QueryId): DropColumnNode;
    protected transformRenameColumn(node: RenameColumnNode, queryId?: QueryId): RenameColumnNode;
    protected transformAlterColumn(node: AlterColumnNode, queryId?: QueryId): AlterColumnNode;
    protected transformModifyColumn(node: ModifyColumnNode, queryId?: QueryId): ModifyColumnNode;
    protected transformAddConstraint(node: AddConstraintNode, queryId?: QueryId): AddConstraintNode;
    protected transformDropConstraint(node: DropConstraintNode, queryId?: QueryId): DropConstraintNode;
    protected transformRenameConstraint(node: RenameConstraintNode, queryId?: QueryId): RenameConstraintNode;
    protected transformCreateView(node: CreateViewNode, queryId?: QueryId): CreateViewNode;
    protected transformRefreshMaterializedView(node: RefreshMaterializedViewNode, queryId?: QueryId): RefreshMaterializedViewNode;
    protected transformDropView(node: DropViewNode, queryId?: QueryId): DropViewNode;
    protected transformGenerated(node: GeneratedNode, queryId?: QueryId): GeneratedNode;
    protected transformDefaultValue(node: DefaultValueNode, queryId?: QueryId): DefaultValueNode;
    protected transformOn(node: OnNode, queryId?: QueryId): OnNode;
    protected transformSelectModifier(node: SelectModifierNode, queryId?: QueryId): SelectModifierNode;
    protected transformCreateType(node: CreateTypeNode, queryId?: QueryId): CreateTypeNode;
    protected transformDropType(node: DropTypeNode, queryId?: QueryId): DropTypeNode;
    protected transformExplain(node: ExplainNode, queryId?: QueryId): ExplainNode;
    protected transformSchemableIdentifier(node: SchemableIdentifierNode, queryId?: QueryId): SchemableIdentifierNode;
    protected transformAggregateFunction(node: AggregateFunctionNode, queryId?: QueryId): AggregateFunctionNode;
    protected transformOver(node: OverNode, queryId?: QueryId): OverNode;
    protected transformPartitionBy(node: PartitionByNode, queryId?: QueryId): PartitionByNode;
    protected transformPartitionByItem(node: PartitionByItemNode, queryId?: QueryId): PartitionByItemNode;
    protected transformBinaryOperation(node: BinaryOperationNode, queryId?: QueryId): BinaryOperationNode;
    protected transformUnaryOperation(node: UnaryOperationNode, queryId?: QueryId): UnaryOperationNode;
    protected transformUsing(node: UsingNode, queryId?: QueryId): UsingNode;
    protected transformFunction(node: FunctionNode, queryId?: QueryId): FunctionNode;
    protected transformCase(node: CaseNode, queryId?: QueryId): CaseNode;
    protected transformWhen(node: WhenNode, queryId?: QueryId): WhenNode;
    protected transformJSONReference(node: JSONReferenceNode, queryId?: QueryId): JSONReferenceNode;
    protected transformJSONPath(node: JSONPathNode, queryId?: QueryId): JSONPathNode;
    protected transformJSONPathLeg(node: JSONPathLegNode, _queryId?: QueryId): JSONPathLegNode;
    protected transformJSONOperatorChain(node: JSONOperatorChainNode, queryId?: QueryId): JSONOperatorChainNode;
    protected transformTuple(node: TupleNode, queryId?: QueryId): TupleNode;
    protected transformMergeQuery(node: MergeQueryNode, queryId?: QueryId): MergeQueryNode;
    protected transformMatched(node: MatchedNode, _queryId?: QueryId): MatchedNode;
    protected transformAddIndex(node: AddIndexNode, queryId?: QueryId): AddIndexNode;
    protected transformCast(node: CastNode, queryId?: QueryId): CastNode;
    protected transformFetch(node: FetchNode, queryId?: QueryId): FetchNode;
    protected transformTop(node: TopNode, _queryId?: QueryId): TopNode;
    protected transformOutput(node: OutputNode, queryId?: QueryId): OutputNode;
    protected transformDataType(node: DataTypeNode, _queryId?: QueryId): DataTypeNode;
    protected transformSelectAll(node: SelectAllNode, _queryId?: QueryId): SelectAllNode;
    protected transformIdentifier(node: IdentifierNode, _queryId?: QueryId): IdentifierNode;
    protected transformValue(node: ValueNode, _queryId?: QueryId): ValueNode;
    protected transformPrimitiveValueList(node: PrimitiveValueListNode, _queryId?: QueryId): PrimitiveValueListNode;
    protected transformOperator(node: OperatorNode, _queryId?: QueryId): OperatorNode;
    protected transformDefaultInsertValue(node: DefaultInsertValueNode, _queryId?: QueryId): DefaultInsertValueNode;
    protected transformOrAction(node: OrActionNode, _queryId?: QueryId): OrActionNode;
    protected transformCollate(node: CollateNode, _queryId?: QueryId): CollateNode;
}
