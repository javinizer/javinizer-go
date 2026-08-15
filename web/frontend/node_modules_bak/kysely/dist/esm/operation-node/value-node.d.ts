import type { OperationNode } from './operation-node.js';
export interface ValueNode extends OperationNode {
    readonly kind: 'ValueNode';
    readonly value: unknown;
    readonly immediate?: boolean;
}
type ValueNodeFactory = Readonly<{
    is(node: OperationNode): node is ValueNode;
    create(value: unknown): Readonly<ValueNode>;
    createImmediate(value: unknown): Readonly<ValueNode>;
}>;
/**
 * @internal
 */
export declare const ValueNode: ValueNodeFactory;
export {};
