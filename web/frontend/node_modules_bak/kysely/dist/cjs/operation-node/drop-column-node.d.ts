import type { OperationNode } from './operation-node.js';
import { ColumnNode } from './column-node.js';
export interface DropColumnNode extends OperationNode {
    readonly kind: 'DropColumnNode';
    readonly column: ColumnNode;
}
type DropColumnNodeFactory = Readonly<{
    is(node: OperationNode): node is DropColumnNode;
    create(column: string): Readonly<DropColumnNode>;
}>;
/**
 * @internal
 */
export declare const DropColumnNode: DropColumnNodeFactory;
export {};
