import { IdentifierNode } from './identifier-node.js';
import type { OperationNode } from './operation-node.js';
export type CreateSchemaNodeParams = Omit<Partial<CreateSchemaNode>, 'kind' | 'schema'>;
export interface CreateSchemaNode extends OperationNode {
    readonly kind: 'CreateSchemaNode';
    readonly schema: IdentifierNode;
    readonly ifNotExists?: boolean;
}
type CreateSchemaNodeFactory = Readonly<{
    is(node: OperationNode): node is CreateSchemaNode;
    create(schema: string, params?: CreateSchemaNodeParams): Readonly<CreateSchemaNode>;
    cloneWith(createSchema: CreateSchemaNode, params: CreateSchemaNodeParams): Readonly<CreateSchemaNode>;
}>;
/**
 * @internal
 */
export declare const CreateSchemaNode: CreateSchemaNodeFactory;
export {};
