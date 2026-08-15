import type { OperationNode } from './operation-node.js';
export interface DefaultInsertValueNode extends OperationNode {
    readonly kind: 'DefaultInsertValueNode';
}
type DefaultInsertValueNodeFactory = Readonly<{
    is(node: OperationNode): node is DefaultInsertValueNode;
    create(): Readonly<DefaultInsertValueNode>;
}>;
/**
 * @internal
 */
export declare const DefaultInsertValueNode: DefaultInsertValueNodeFactory;
export {};
