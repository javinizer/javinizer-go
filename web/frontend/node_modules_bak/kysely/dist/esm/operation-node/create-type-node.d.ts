import type { OperationNode } from './operation-node.js';
import type { SchemableIdentifierNode } from './schemable-identifier-node.js';
import { ValueListNode } from './value-list-node.js';
export type CreateTypeNodeParams = Omit<Partial<CreateTypeNode>, 'kind'>;
export interface CreateTypeNode extends OperationNode {
    readonly kind: 'CreateTypeNode';
    readonly name: SchemableIdentifierNode;
    readonly enum?: ValueListNode;
}
type CreateTypeNodeFactory = Readonly<{
    is(node: OperationNode): node is CreateTypeNode;
    create(name: SchemableIdentifierNode): Readonly<CreateTypeNode>;
    cloneWithEnum(createType: CreateTypeNode, values: readonly string[]): Readonly<CreateTypeNode>;
}>;
/**
 * @internal
 */
export declare const CreateTypeNode: CreateTypeNodeFactory;
export {};
