import type { OperationNode } from './operation-node.js';
export type TopModifier = 'percent' | 'with ties' | 'percent with ties';
export interface TopNode extends OperationNode {
    readonly kind: 'TopNode';
    readonly expression: number | bigint;
    readonly modifiers?: TopModifier;
}
type TopNodeFactory = Readonly<{
    is(node: OperationNode): node is TopNode;
    create(expression: number | bigint, modifiers?: TopModifier): Readonly<TopNode>;
}>;
/**
 * @internal
 */
export declare const TopNode: TopNodeFactory;
export {};
