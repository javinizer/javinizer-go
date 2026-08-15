import { IdentifierNode } from './identifier-node.js';
import type { OperationNode } from './operation-node.js';
export interface SchemableIdentifierNode extends OperationNode {
    readonly kind: 'SchemableIdentifierNode';
    readonly schema?: IdentifierNode;
    readonly identifier: IdentifierNode;
}
type SchemableIdentifierNodeFactory = Readonly<{
    is(node: OperationNode): node is SchemableIdentifierNode;
    create(identifier: string): Readonly<SchemableIdentifierNode>;
    createWithSchema(schema: string, identifier: string): Readonly<SchemableIdentifierNode>;
}>;
/**
 * @internal
 */
export declare const SchemableIdentifierNode: SchemableIdentifierNodeFactory;
export {};
