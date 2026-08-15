import type { OperationNode } from './operation-node.js';
import type { SchemableIdentifierNode } from './schemable-identifier-node.js';
export type DropTypeNodeParams = Omit<Partial<DropTypeNode>, 'kind' | 'name'>;
export interface DropTypeNode extends OperationNode {
    readonly kind: 'DropTypeNode';
    readonly name: SchemableIdentifierNode;
    readonly ifExists?: boolean;
}
type DropTypeNodeFactory = Readonly<{
    is(node: OperationNode): node is DropTypeNode;
    create(name: SchemableIdentifierNode): Readonly<DropTypeNode>;
    cloneWith(dropType: DropTypeNode, params: DropTypeNodeParams): Readonly<DropTypeNode>;
}>;
/**
 * @internal
 */
export declare const DropTypeNode: DropTypeNodeFactory;
export {};
