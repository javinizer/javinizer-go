import type { OperationNode } from './operation-node.js';
import type { OperatorNode } from './operator-node.js';
import type { ValueNode } from './value-node.js';
export interface JSONOperatorChainNode extends OperationNode {
    readonly kind: 'JSONOperatorChainNode';
    readonly operator: OperatorNode;
    readonly values: readonly ValueNode[];
}
type JSONOperatorChainNodeFactory = Readonly<{
    is(node: OperationNode): node is JSONOperatorChainNode;
    create(operator: OperatorNode): Readonly<JSONOperatorChainNode>;
    cloneWithValue(node: JSONOperatorChainNode, value: ValueNode): Readonly<JSONOperatorChainNode>;
}>;
/**
 * @internal
 */
export declare const JSONOperatorChainNode: JSONOperatorChainNodeFactory;
export {};
