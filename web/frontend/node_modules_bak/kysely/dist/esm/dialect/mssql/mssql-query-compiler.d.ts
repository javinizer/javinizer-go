import type { AddColumnNode } from '../../operation-node/add-column-node.js';
import type { AlterTableColumnAlterationNode } from '../../operation-node/alter-table-node.js';
import type { DropColumnNode } from '../../operation-node/drop-column-node.js';
import type { OffsetNode } from '../../operation-node/offset-node.js';
import type { MergeQueryNode } from '../../operation-node/merge-query-node.js';
import { DefaultQueryCompiler } from '../../query-compiler/default-query-compiler.js';
import type { CollateNode } from '../../operation-node/collate-node.js';
export declare class MssqlQueryCompiler extends DefaultQueryCompiler {
    protected getCurrentParameterPlaceholder(): string;
    protected visitOffset(node: OffsetNode): void;
    protected compileColumnAlterations(columnAlterations: readonly AlterTableColumnAlterationNode[]): void;
    protected visitAddColumn(node: AddColumnNode): void;
    protected visitDropColumn(node: DropColumnNode): void;
    protected visitMergeQuery(node: MergeQueryNode): void;
    protected visitCollate(node: CollateNode): void;
    protected announcesNewColumnDataType(): boolean;
}
