import type { Expression } from '../expression/expression.js';
import type { OperationNode } from '../operation-node/operation-node.js';
export type DefaultValueExpression = unknown | Expression<unknown>;
export declare function parseDefaultValueExpression(value: DefaultValueExpression): OperationNode;
