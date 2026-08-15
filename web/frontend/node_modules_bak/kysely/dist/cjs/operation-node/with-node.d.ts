import type { OperationNode } from './operation-node.js';
import type { CommonTableExpressionNode } from './common-table-expression-node.js';
export type WithNodeParams = Omit<WithNode, 'kind' | 'expressions'>;
export interface WithNode extends OperationNode {
    readonly kind: 'WithNode';
    readonly expressions: ReadonlyArray<CommonTableExpressionNode>;
    readonly recursive?: boolean;
}
type WithNodeFactory = Readonly<{
    is(node: OperationNode): node is WithNode;
    create(expression: CommonTableExpressionNode, params?: WithNodeParams): Readonly<WithNode>;
    cloneWithExpression(withNode: WithNode, expression: CommonTableExpressionNode): Readonly<WithNode>;
}>;
/**
 * @internal
 */
export declare const WithNode: WithNodeFactory;
export {};
