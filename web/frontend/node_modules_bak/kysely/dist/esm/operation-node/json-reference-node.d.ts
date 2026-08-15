import type { JSONOperatorChainNode } from './json-operator-chain-node.js';
import type { JSONPathNode } from './json-path-node.js';
import type { OperationNode } from './operation-node.js';
import type { ReferenceNode } from './reference-node.js';
export interface JSONReferenceNode extends OperationNode {
    readonly kind: 'JSONReferenceNode';
    readonly reference: ReferenceNode;
    readonly traversal: JSONPathNode | JSONOperatorChainNode;
}
type JSONReferenceNodeFactory = Readonly<{
    is(node: OperationNode): node is JSONReferenceNode;
    create(reference: ReferenceNode, traversal: JSONPathNode | JSONOperatorChainNode): Readonly<JSONReferenceNode>;
    cloneWithTraversal(node: JSONReferenceNode, traversal: JSONPathNode | JSONOperatorChainNode): Readonly<JSONReferenceNode>;
}>;
/**
 * @internal
 */
export declare const JSONReferenceNode: JSONReferenceNodeFactory;
export {};
