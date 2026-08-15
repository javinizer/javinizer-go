import type { OperationNode } from './operation-node.js';
export interface TupleNode extends OperationNode {
    readonly kind: 'TupleNode';
    readonly values: ReadonlyArray<OperationNode>;
}
type TupleNodeFactory = Readonly<{
    is(node: OperationNode): node is TupleNode;
    create(values: ReadonlyArray<OperationNode>): Readonly<TupleNode>;
}>;
/**
 * @internal
 */
export declare const TupleNode: TupleNodeFactory;
export {};
