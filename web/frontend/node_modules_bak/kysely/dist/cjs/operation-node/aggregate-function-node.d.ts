import type { OperationNode } from './operation-node.js';
import type { OverNode } from './over-node.js';
import { WhereNode } from './where-node.js';
import { OrderByNode } from './order-by-node.js';
import type { OrderByItemNode } from './order-by-item-node.js';
export interface AggregateFunctionNode extends OperationNode {
    readonly kind: 'AggregateFunctionNode';
    readonly func: string;
    readonly aggregated: readonly OperationNode[];
    readonly distinct?: boolean;
    readonly orderBy?: OrderByNode;
    readonly withinGroup?: OrderByNode;
    readonly filter?: WhereNode;
    readonly over?: OverNode;
}
type AggregateFunctionNodeFactory = Readonly<{
    is(node: OperationNode): node is AggregateFunctionNode;
    create(aggregateFunction: string, aggregated?: readonly OperationNode[]): Readonly<AggregateFunctionNode>;
    cloneWithDistinct(aggregateFunctionNode: AggregateFunctionNode): Readonly<AggregateFunctionNode>;
    cloneWithOrderBy(aggregateFunctionNode: AggregateFunctionNode, orderItems: ReadonlyArray<OrderByItemNode>, withinGroup?: boolean): Readonly<AggregateFunctionNode>;
    cloneWithFilter(aggregateFunctionNode: AggregateFunctionNode, filter: OperationNode): Readonly<AggregateFunctionNode>;
    cloneWithOrFilter(aggregateFunctionNode: AggregateFunctionNode, filter: OperationNode): Readonly<AggregateFunctionNode>;
    cloneWithOver(aggregateFunctionNode: AggregateFunctionNode, over?: OverNode): Readonly<AggregateFunctionNode>;
}>;
/**
 * @internal
 */
export declare const AggregateFunctionNode: AggregateFunctionNodeFactory;
export {};
