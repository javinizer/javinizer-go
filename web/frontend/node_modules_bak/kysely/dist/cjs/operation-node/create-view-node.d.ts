import type { ColumnNode } from './column-node.js';
import type { OperationNode } from './operation-node.js';
import type { RawNode } from './raw-node.js';
import { SchemableIdentifierNode } from './schemable-identifier-node.js';
import type { SelectQueryNode } from './select-query-node.js';
export type CreateViewNodeParams = Omit<Partial<CreateViewNode>, 'kind' | 'name'>;
export interface CreateViewNode extends OperationNode {
    readonly kind: 'CreateViewNode';
    readonly name: SchemableIdentifierNode;
    readonly temporary?: boolean;
    readonly materialized?: boolean;
    readonly orReplace?: boolean;
    readonly ifNotExists?: boolean;
    readonly columns?: ReadonlyArray<ColumnNode>;
    readonly as?: SelectQueryNode | RawNode;
}
type CreateViewNodeFactory = Readonly<{
    is(node: OperationNode): node is CreateViewNode;
    create(name: string): Readonly<CreateViewNode>;
    cloneWith(createView: CreateViewNode, params: CreateViewNodeParams): Readonly<CreateViewNode>;
}>;
/**
 * @internal
 */
export declare const CreateViewNode: CreateViewNodeFactory;
export {};
