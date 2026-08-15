import type { OperationNode } from './operation-node.js';
import { SchemableIdentifierNode } from './schemable-identifier-node.js';
export interface TableNode extends OperationNode {
    readonly kind: 'TableNode';
    readonly table: SchemableIdentifierNode;
}
type TableNodeFactory = Readonly<{
    is(node: OperationNode): node is TableNode;
    create(table: string): Readonly<TableNode>;
    createWithSchema(schema: string, table: string): Readonly<TableNode>;
}>;
/**
 * @internal
 */
export declare const TableNode: TableNodeFactory;
export {};
