import type { BinaryOperationNode } from '../../operation-node/binary-operation-node.js';
import { OperationNodeTransformer } from '../../operation-node/operation-node-transformer.js';
import type { EmptyInListsStrategy } from './handle-empty-in-lists.js';
export declare class HandleEmptyInListsTransformer extends OperationNodeTransformer {
    #private;
    constructor(strategy: EmptyInListsStrategy);
    protected transformBinaryOperation(node: BinaryOperationNode): BinaryOperationNode;
}
