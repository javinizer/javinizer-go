import type { JSONPathLegNode } from './json-path-leg-node.js';
import type { OperationNode } from './operation-node.js';
import type { OperatorNode } from './operator-node.js';
export interface JSONPathNode extends OperationNode {
    readonly kind: 'JSONPathNode';
    readonly inOperator?: OperatorNode;
    readonly pathLegs: ReadonlyArray<JSONPathLegNode>;
}
type JSONPathNodeFactory = Readonly<{
    is(node: OperationNode): node is JSONPathNode;
    create(inOperator?: OperatorNode): Readonly<JSONPathNode>;
    cloneWithLeg(jsonPathNode: JSONPathNode, pathLeg: JSONPathLegNode): Readonly<JSONPathNode>;
}>;
/**
 * @internal
 */
export declare const JSONPathNode: JSONPathNodeFactory;
export {};
