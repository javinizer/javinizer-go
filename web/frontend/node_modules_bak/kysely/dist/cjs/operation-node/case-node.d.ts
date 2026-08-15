import type { OperationNode } from './operation-node.js';
import { WhenNode } from './when-node.js';
export interface CaseNode extends OperationNode {
    readonly kind: 'CaseNode';
    readonly value?: OperationNode;
    readonly when?: ReadonlyArray<WhenNode>;
    readonly else?: OperationNode;
    readonly isStatement?: boolean;
}
type CaseNodeFactory = Readonly<{
    is(node: OperationNode): node is CaseNode;
    create(value?: OperationNode): Readonly<CaseNode>;
    cloneWithWhen(caseNode: CaseNode, when: WhenNode): Readonly<CaseNode>;
    cloneWithThen(caseNode: CaseNode, then: OperationNode): Readonly<CaseNode>;
    cloneWith(caseNode: CaseNode, props: Partial<Pick<CaseNode, 'else' | 'isStatement'>>): Readonly<CaseNode>;
}>;
/**
 * @internal
 */
export declare const CaseNode: CaseNodeFactory;
export {};
