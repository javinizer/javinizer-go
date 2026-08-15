import type { OperationNode } from './operation-node.js';
import type { TableNode } from './table-node.js';
export type DropTablexNodeParams = Omit<Partial<DropTableNode>, 'kind' | 'table'>;
export interface DropTableNode extends OperationNode {
    readonly kind: 'DropTableNode';
    readonly table: TableNode;
    readonly ifExists?: boolean;
    readonly cascade?: boolean;
}
type DropTableNodeFactory = Readonly<{
    is(node: OperationNode): node is DropTableNode;
    create(table: TableNode, params?: DropTablexNodeParams): Readonly<DropTableNode>;
    cloneWith(dropIndex: DropTableNode, params: DropTablexNodeParams): Readonly<DropTableNode>;
}>;
/**
 * @internal
 */
export declare const DropTableNode: DropTableNodeFactory;
export {};
