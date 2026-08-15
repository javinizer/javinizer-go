import type { OperationNode } from './operation-node.js';
import { SchemableIdentifierNode } from './schemable-identifier-node.js';
import type { TableNode } from './table-node.js';
export type DropIndexNodeProps = Omit<DropIndexNode, 'kind' | 'name'>;
export interface DropIndexNode extends OperationNode {
    readonly kind: 'DropIndexNode';
    readonly name: SchemableIdentifierNode;
    readonly table?: TableNode;
    readonly ifExists?: boolean;
    readonly cascade?: boolean;
}
type DropIndexNodeFactory = Readonly<{
    is(node: OperationNode): node is DropIndexNode;
    create(name: string, params?: DropIndexNodeProps): Readonly<DropIndexNode>;
    cloneWith(dropIndex: DropIndexNode, props: DropIndexNodeProps): Readonly<DropIndexNode>;
}>;
/**
 * @internal
 */
export declare const DropIndexNode: DropIndexNodeFactory;
export {};
