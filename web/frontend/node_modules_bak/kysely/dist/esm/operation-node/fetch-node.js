/// <reference types="./fetch-node.d.ts" />
import { freeze } from '../util/object-utils.js';
import { ValueNode } from './value-node.js';
/**
 * @internal
 */
export const FetchNode = freeze({
    is(node) {
        return node.kind === 'FetchNode';
    },
    create(rowCount, modifier) {
        return {
            kind: 'FetchNode',
            rowCount: ValueNode.create(rowCount),
            modifier,
        };
    },
});
