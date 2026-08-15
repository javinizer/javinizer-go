import type { OperationNode } from './operation-node.js';
export interface RawNode extends OperationNode {
    readonly kind: 'RawNode';
    readonly sqlFragments: ReadonlyArray<string>;
    readonly parameters: ReadonlyArray<OperationNode>;
}
type RawNodeFactory = Readonly<{
    is(node: OperationNode): node is RawNode;
    create(sqlFragments: ReadonlyArray<string>, parameters: ReadonlyArray<OperationNode>): Readonly<RawNode>;
    createWithSql(sql: string): Readonly<RawNode>;
    createWithChild(child: OperationNode): Readonly<RawNode>;
    createWithChildren(children: ReadonlyArray<OperationNode>): Readonly<RawNode>;
}>;
/**
 * @internal
 */
export declare const RawNode: RawNodeFactory;
export {};
