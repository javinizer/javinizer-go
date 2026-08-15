import type { OperationNode } from './operation-node.js';
import { SchemableIdentifierNode } from './schemable-identifier-node.js';
export type RefreshMaterializedViewNodeParams = Omit<Partial<RefreshMaterializedViewNode>, 'kind' | 'name'>;
export interface RefreshMaterializedViewNode extends OperationNode {
    readonly kind: 'RefreshMaterializedViewNode';
    readonly name: SchemableIdentifierNode;
    readonly concurrently?: boolean;
    readonly withNoData?: boolean;
}
type RefreshMaterializedViewNodeFactory = Readonly<{
    is(node: OperationNode): node is RefreshMaterializedViewNode;
    create(name: string): Readonly<RefreshMaterializedViewNode>;
    cloneWith(createView: RefreshMaterializedViewNode, params: RefreshMaterializedViewNodeParams): Readonly<RefreshMaterializedViewNode>;
}>;
/**
 * @internal
 */
export declare const RefreshMaterializedViewNode: RefreshMaterializedViewNodeFactory;
export {};
