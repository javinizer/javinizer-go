import type { CreateIndexNode } from '../../operation-node/create-index-node.js';
import { DefaultQueryCompiler } from '../../query-compiler/default-query-compiler.js';
export declare class MysqlQueryCompiler extends DefaultQueryCompiler {
    protected getCurrentParameterPlaceholder(): string;
    protected getLeftExplainOptionsWrapper(): string;
    protected getExplainOptionAssignment(): string;
    protected getExplainOptionsDelimiter(): string;
    protected getRightExplainOptionsWrapper(): string;
    protected getLeftIdentifierWrapper(): string;
    protected getRightIdentifierWrapper(): string;
    protected sanitizeIdentifier(identifier: string): string;
    /**
     * MySQL requires escaping backslashes in string literals when using the
     * default NO_BACKSLASH_ESCAPES=OFF mode. Without this, a backslash
     * followed by a quote (\') can break out of the string literal.
     *
     * @see https://dev.mysql.com/doc/refman/9.6/en/string-literals.html
     */
    protected sanitizeStringLiteral(value: string): string;
    /**
     * Member values appear inside `"..."` in the JSON path, which itself sits
     * inside a SQL string literal. They must therefore be escaped twice — once
     * for the JSON path grammar, then again for MySQL's string literal parser.
     */
    protected sanitizeJSONPathMemberValue(value: string): string;
    protected visitCreateIndex(node: CreateIndexNode): void;
}
