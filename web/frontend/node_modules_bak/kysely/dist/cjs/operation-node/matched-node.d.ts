import type { OperationNode } from './operation-node.js';
export interface MatchedNode extends OperationNode {
    readonly kind: 'MatchedNode';
    readonly not: boolean;
    readonly bySource: boolean;
}
type MatchedNodeFactory = Readonly<{
    is(node: OperationNode): node is MatchedNode;
    create(not: boolean, bySource?: boolean): Readonly<MatchedNode>;
}>;
/**
 * @internal
 */
export declare const MatchedNode: MatchedNodeFactory;
export {};
