import type { Expression } from '../expression/expression.js';
import { type ColumnDataType } from '../operation-node/data-type-node.js';
import type { OperationNode } from '../operation-node/operation-node.js';
export type DataTypeExpression = ColumnDataType | Expression<any>;
export declare function parseDataTypeExpression(dataType: DataTypeExpression): OperationNode;
