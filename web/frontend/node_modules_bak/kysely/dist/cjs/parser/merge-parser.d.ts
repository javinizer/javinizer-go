import type { InsertQueryNode } from '../operation-node/insert-query-node.js';
import { type OperationNodeSource } from '../operation-node/operation-node-source.js';
import type { OperationNode } from '../operation-node/operation-node.js';
import { WhenNode } from '../operation-node/when-node.js';
export declare function parseMergeWhen(type: {
    isMatched: boolean;
    bySource?: boolean;
}, args?: any[], refRight?: boolean): WhenNode;
export declare function parseMergeThen(result: 'delete' | 'do nothing' | OperationNodeSource | InsertQueryNode): OperationNode;
