import type { OperationNode } from './operation-node.js';
import type { OrderByItemNode } from './order-by-item-node.js';
export interface OrderByNode extends OperationNode {
    readonly kind: 'OrderByNode';
    readonly items: ReadonlyArray<OrderByItemNode>;
}
type OrderByNodeFactory = Readonly<{
    is(node: OperationNode): node is OrderByNode;
    create(items: ReadonlyArray<OrderByItemNode>): Readonly<OrderByNode>;
    cloneWithItems(orderBy: OrderByNode, items: ReadonlyArray<OrderByItemNode>): Readonly<OrderByNode>;
}>;
/**
 * @internal
 */
export declare const OrderByNode: OrderByNodeFactory;
export {};
