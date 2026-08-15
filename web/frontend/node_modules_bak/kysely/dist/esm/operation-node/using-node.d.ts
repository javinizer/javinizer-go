import type { OperationNode } from './operation-node.js';
export interface UsingNode extends OperationNode {
    readonly kind: 'UsingNode';
    readonly tables: ReadonlyArray<OperationNode>;
}
type UsingNodeFactory = Readonly<{
    is(node: OperationNode): node is UsingNode;
    create(tables: ReadonlyArray<OperationNode>): Readonly<UsingNode>;
    cloneWithTables(using: UsingNode, tables: ReadonlyArray<OperationNode>): Readonly<UsingNode>;
}>;
/**
 * @internal
 */
export declare const UsingNode: UsingNodeFactory;
export {};
