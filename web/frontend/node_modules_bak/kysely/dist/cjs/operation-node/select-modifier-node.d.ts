import type { OperationNode } from './operation-node.js';
export type SelectModifier = 'ForUpdate' | 'ForNoKeyUpdate' | 'ForShare' | 'ForKeyShare' | 'NoWait' | 'SkipLocked' | 'Distinct';
export interface SelectModifierNode extends OperationNode {
    readonly kind: 'SelectModifierNode';
    readonly modifier?: SelectModifier;
    readonly rawModifier?: OperationNode;
    readonly of?: ReadonlyArray<OperationNode>;
}
type SelectModifierNodeFactory = Readonly<{
    is(node: OperationNode): node is SelectModifierNode;
    create(modifier: SelectModifier, of?: ReadonlyArray<OperationNode>): Readonly<SelectModifierNode>;
    createWithExpression(modifier: OperationNode): Readonly<SelectModifierNode>;
}>;
/**
 * @internal
 */
export declare const SelectModifierNode: SelectModifierNodeFactory;
export {};
