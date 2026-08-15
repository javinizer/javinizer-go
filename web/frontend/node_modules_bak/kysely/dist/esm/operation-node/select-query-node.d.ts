import { FromNode } from './from-node.js';
import type { GroupByItemNode } from './group-by-item-node.js';
import { GroupByNode } from './group-by-node.js';
import { HavingNode } from './having-node.js';
import type { JoinNode } from './join-node.js';
import type { LimitNode } from './limit-node.js';
import type { OffsetNode } from './offset-node.js';
import type { OperationNode } from './operation-node.js';
import type { OrderByItemNode } from './order-by-item-node.js';
import type { OrderByNode } from './order-by-node.js';
import type { SelectionNode } from './selection-node.js';
import type { WhereNode } from './where-node.js';
import type { WithNode } from './with-node.js';
import type { SelectModifierNode } from './select-modifier-node.js';
import type { ExplainNode } from './explain-node.js';
import type { SetOperationNode } from './set-operation-node.js';
import type { FetchNode } from './fetch-node.js';
import type { TopNode } from './top-node.js';
export interface SelectQueryNode extends OperationNode {
    readonly kind: 'SelectQueryNode';
    readonly from?: FromNode;
    readonly selections?: ReadonlyArray<SelectionNode>;
    readonly distinctOn?: ReadonlyArray<OperationNode>;
    readonly joins?: ReadonlyArray<JoinNode>;
    readonly groupBy?: GroupByNode;
    readonly orderBy?: OrderByNode;
    readonly where?: WhereNode;
    readonly frontModifiers?: ReadonlyArray<SelectModifierNode>;
    readonly endModifiers?: ReadonlyArray<SelectModifierNode>;
    readonly limit?: LimitNode;
    readonly offset?: OffsetNode;
    readonly with?: WithNode;
    readonly having?: HavingNode;
    readonly explain?: ExplainNode;
    readonly setOperations?: ReadonlyArray<SetOperationNode>;
    readonly fetch?: FetchNode;
    readonly top?: TopNode;
}
type SelectQueryNodeFactory = Readonly<{
    is(node: OperationNode): node is SelectQueryNode;
    create(withNode?: WithNode): Readonly<SelectQueryNode>;
    createFrom(fromItems: ReadonlyArray<OperationNode>, withNode?: WithNode): Readonly<SelectQueryNode>;
    cloneWithSelections(select: SelectQueryNode, selections: ReadonlyArray<SelectionNode>): Readonly<SelectQueryNode>;
    cloneWithDistinctOn(select: SelectQueryNode, expressions: ReadonlyArray<OperationNode>): Readonly<SelectQueryNode>;
    cloneWithFrontModifier(select: SelectQueryNode, modifier: SelectModifierNode): Readonly<SelectQueryNode>;
    cloneWithOrderByItems(node: SelectQueryNode, items: ReadonlyArray<OrderByItemNode>): Readonly<SelectQueryNode>;
    cloneWithGroupByItems(selectNode: SelectQueryNode, items: ReadonlyArray<GroupByItemNode>): Readonly<SelectQueryNode>;
    cloneWithLimit(selectNode: SelectQueryNode, limit: LimitNode): Readonly<SelectQueryNode>;
    cloneWithOffset(selectNode: SelectQueryNode, offset: OffsetNode): Readonly<SelectQueryNode>;
    cloneWithFetch(selectNode: SelectQueryNode, fetch: FetchNode): Readonly<SelectQueryNode>;
    cloneWithHaving(selectNode: SelectQueryNode, operation: OperationNode): Readonly<SelectQueryNode>;
    cloneWithSetOperations(selectNode: SelectQueryNode, setOperations: ReadonlyArray<SetOperationNode>): Readonly<SelectQueryNode>;
    cloneWithoutSelections(select: SelectQueryNode): Readonly<SelectQueryNode>;
    cloneWithoutLimit(select: SelectQueryNode): Readonly<SelectQueryNode>;
    cloneWithoutOffset(select: SelectQueryNode): Readonly<SelectQueryNode>;
    cloneWithoutOrderBy(node: SelectQueryNode): Readonly<SelectQueryNode>;
    cloneWithoutGroupBy(select: SelectQueryNode): Readonly<SelectQueryNode>;
}>;
/**
 * @internal
 */
export declare const SelectQueryNode: SelectQueryNodeFactory;
export {};
