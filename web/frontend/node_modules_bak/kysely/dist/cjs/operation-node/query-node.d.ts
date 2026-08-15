import { InsertQueryNode } from './insert-query-node.js';
import { SelectQueryNode } from './select-query-node.js';
import { UpdateQueryNode } from './update-query-node.js';
import { DeleteQueryNode } from './delete-query-node.js';
import { WhereNode } from './where-node.js';
import type { JoinNode } from './join-node.js';
import type { SelectionNode } from './selection-node.js';
import { ReturningNode } from './returning-node.js';
import type { OperationNode } from './operation-node.js';
import { ExplainNode } from './explain-node.js';
import type { ExplainFormat } from '../util/explainable.js';
import type { Expression } from '../expression/expression.js';
import { MergeQueryNode } from './merge-query-node.js';
import type { TopNode } from './top-node.js';
import { OutputNode } from './output-node.js';
import { OrderByNode } from './order-by-node.js';
import type { OrderByItemNode } from './order-by-item-node.js';
export type QueryNode = SelectQueryNode | InsertQueryNode | UpdateQueryNode | DeleteQueryNode | MergeQueryNode;
type HasJoins = {
    joins?: ReadonlyArray<JoinNode>;
};
type HasWhere = {
    where?: WhereNode;
};
type HasReturning = {
    returning?: ReturningNode;
};
type HasExplain = {
    explain?: ExplainNode;
};
type HasTop = {
    top?: TopNode;
};
type HasOutput = {
    output?: OutputNode;
};
type HasEndModifiers = {
    endModifiers?: ReadonlyArray<OperationNode>;
};
type HasOrderBy = {
    orderBy?: OrderByNode;
};
type QueryNodeFactory = Readonly<{
    is(node: OperationNode): node is QueryNode;
    cloneWithEndModifier<T extends HasEndModifiers>(node: T, modifier: OperationNode): Readonly<T>;
    cloneWithWhere<T extends HasWhere>(node: T, operation: OperationNode): Readonly<T>;
    cloneWithJoin<T extends HasJoins>(node: T, join: JoinNode): Readonly<T>;
    cloneWithReturning<T extends HasReturning>(node: T, selections: ReadonlyArray<SelectionNode>): Readonly<T>;
    cloneWithoutReturning<T extends HasReturning>(node: T): Readonly<T>;
    cloneWithoutWhere<T extends HasWhere>(node: T): Readonly<T>;
    cloneWithExplain<T extends HasExplain>(node: T, format: ExplainFormat | undefined, options: Expression<any> | undefined): Readonly<T>;
    cloneWithTop<T extends HasTop>(node: T, top: TopNode): Readonly<T>;
    cloneWithOutput<T extends HasOutput>(node: T, selections: ReadonlyArray<SelectionNode>): Readonly<T>;
    cloneWithOrderByItems<T extends HasOrderBy>(node: T, items: ReadonlyArray<OrderByItemNode>): Readonly<T>;
    cloneWithoutOrderBy<T extends HasOrderBy>(node: T): Readonly<T>;
}>;
/**
 * @internal
 */
export declare const QueryNode: QueryNodeFactory;
export {};
