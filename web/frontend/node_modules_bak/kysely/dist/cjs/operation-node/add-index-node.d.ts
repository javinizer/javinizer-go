import { IdentifierNode } from './identifier-node.js';
import type { OperationNode } from './operation-node.js';
import type { RawNode } from './raw-node.js';
export type AddIndexNodeProps = Omit<AddIndexNode, 'kind' | 'name'>;
export interface AddIndexNode extends OperationNode {
    readonly kind: 'AddIndexNode';
    readonly name: IdentifierNode;
    readonly columns?: OperationNode[];
    readonly unique?: boolean;
    readonly using?: RawNode;
    readonly ifNotExists?: boolean;
}
type AddIndexNodeFactory = Readonly<{
    is(node: OperationNode): node is AddIndexNode;
    create(name: string): Readonly<AddIndexNode>;
    cloneWith(node: AddIndexNode, props: Omit<AddIndexNode, 'kind' | 'name'>): Readonly<AddIndexNode>;
    cloneWithColumns(node: AddIndexNode, columns: OperationNode[]): Readonly<AddIndexNode>;
}>;
/**
 * @internal
 */
export declare const AddIndexNode: AddIndexNodeFactory;
export {};
