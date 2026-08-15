import type { OperationNode } from './operation-node.js';
export interface FunctionNode extends OperationNode {
    readonly kind: 'FunctionNode';
    readonly func: string;
    readonly arguments: ReadonlyArray<OperationNode>;
}
type FunctionNodeFactory = Readonly<{
    is(node: OperationNode): node is FunctionNode;
    create(func: string, args: ReadonlyArray<OperationNode>): Readonly<FunctionNode>;
}>;
/**
 * @internal
 */
export declare const FunctionNode: FunctionNodeFactory;
export {};
