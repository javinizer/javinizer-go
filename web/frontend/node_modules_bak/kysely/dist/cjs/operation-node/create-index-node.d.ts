import { IdentifierNode } from './identifier-node.js';
import type { OperationNode } from './operation-node.js';
import type { RawNode } from './raw-node.js';
import type { TableNode } from './table-node.js';
import type { WhereNode } from './where-node.js';
export type CreateIndexNodeProps = Omit<CreateIndexNode, 'kind' | 'name'>;
export type IndexType = 'btree' | 'hash' | 'gist' | 'gin';
export interface CreateIndexNode extends OperationNode {
    readonly kind: 'CreateIndexNode';
    readonly name: IdentifierNode;
    readonly table?: TableNode;
    readonly columns?: OperationNode[];
    readonly unique?: boolean;
    readonly using?: RawNode;
    readonly ifNotExists?: boolean;
    readonly where?: WhereNode;
    readonly nullsNotDistinct?: boolean;
}
type CreateIndexNodeFactory = Readonly<{
    is(node: OperationNode): node is CreateIndexNode;
    create(name: string): Readonly<CreateIndexNode>;
    cloneWith(node: CreateIndexNode, props: CreateIndexNodeProps): Readonly<CreateIndexNode>;
    cloneWithColumns(node: CreateIndexNode, columns: OperationNode[]): Readonly<CreateIndexNode>;
}>;
/**
 * @internal
 */
export declare const CreateIndexNode: CreateIndexNodeFactory;
export {};
