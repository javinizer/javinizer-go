import { IdentifierNode } from './identifier-node.js';
import type { OperationNode } from './operation-node.js';
export interface ColumnNode extends OperationNode {
    readonly kind: 'ColumnNode';
    readonly column: IdentifierNode;
}
type ColumnNodeFactory = Readonly<{
    is(node: OperationNode): node is ColumnNode;
    create(column: string): Readonly<ColumnNode>;
}>;
/**
 * @internal
 */
export declare const ColumnNode: ColumnNodeFactory;
export {};
