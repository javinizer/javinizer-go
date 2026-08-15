import type { OperationNode } from './operation-node.js';
export interface SelectAllNode extends OperationNode {
    readonly kind: 'SelectAllNode';
}
type SelectAllNodeFactory = Readonly<{
    is(node: OperationNode): node is SelectAllNode;
    create(): Readonly<SelectAllNode>;
}>;
/**
 * @internal
 */
export declare const SelectAllNode: SelectAllNodeFactory;
export {};
