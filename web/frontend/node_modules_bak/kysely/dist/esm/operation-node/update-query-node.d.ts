import type { ColumnUpdateNode } from './column-update-node.js';
import type { JoinNode } from './join-node.js';
import type { OperationNode } from './operation-node.js';
import type { ReturningNode } from './returning-node.js';
import type { WhereNode } from './where-node.js';
import type { WithNode } from './with-node.js';
import { FromNode } from './from-node.js';
import type { ExplainNode } from './explain-node.js';
import type { LimitNode } from './limit-node.js';
import type { TopNode } from './top-node.js';
import type { OutputNode } from './output-node.js';
import type { OrderByNode } from './order-by-node.js';
export interface UpdateQueryNode extends OperationNode {
    readonly kind: 'UpdateQueryNode';
    readonly table?: OperationNode;
    readonly from?: FromNode;
    readonly joins?: ReadonlyArray<JoinNode>;
    readonly where?: WhereNode;
    readonly updates?: ReadonlyArray<ColumnUpdateNode>;
    readonly returning?: ReturningNode;
    readonly with?: WithNode;
    readonly explain?: ExplainNode;
    readonly endModifiers?: ReadonlyArray<OperationNode>;
    readonly limit?: LimitNode;
    readonly top?: TopNode;
    readonly output?: OutputNode;
    readonly orderBy?: OrderByNode;
}
type UpdateQueryNodeFactory = Readonly<{
    is(node: OperationNode): node is UpdateQueryNode;
    create(tables: ReadonlyArray<OperationNode>, withNode?: WithNode): Readonly<UpdateQueryNode>;
    createWithoutTable(): Readonly<UpdateQueryNode>;
    cloneWithFromItems(updateQuery: UpdateQueryNode, fromItems: ReadonlyArray<OperationNode>): Readonly<UpdateQueryNode>;
    cloneWithUpdates(updateQuery: UpdateQueryNode, updates: ReadonlyArray<ColumnUpdateNode>): Readonly<UpdateQueryNode>;
    cloneWithLimit(updateQuery: UpdateQueryNode, limit: LimitNode): Readonly<UpdateQueryNode>;
}>;
/**
 * @internal
 */
export declare const UpdateQueryNode: UpdateQueryNodeFactory;
export {};
