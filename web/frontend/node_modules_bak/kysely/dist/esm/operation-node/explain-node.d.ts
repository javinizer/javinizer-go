import type { ExplainFormat } from '../util/explainable.js';
import type { OperationNode } from './operation-node.js';
export interface ExplainNode extends OperationNode {
    readonly kind: 'ExplainNode';
    readonly format?: ExplainFormat;
    readonly options?: OperationNode;
}
type ExplainNodeFactory = Readonly<{
    is(node: OperationNode): node is ExplainNode;
    create(format?: ExplainFormat, options?: OperationNode): Readonly<ExplainNode>;
}>;
/**
 * @internal
 */
export declare const ExplainNode: ExplainNodeFactory;
export {};
