import type { OperationNode } from './operation-node.js';
export interface DefaultValueNode extends OperationNode {
    readonly kind: 'DefaultValueNode';
    readonly defaultValue: OperationNode;
}
type DefaultValueNodeFactory = Readonly<{
    is(node: OperationNode): node is DefaultValueNode;
    create(defaultValue: OperationNode): Readonly<DefaultValueNode>;
}>;
/**
 * @internal
 */
export declare const DefaultValueNode: DefaultValueNodeFactory;
export {};
