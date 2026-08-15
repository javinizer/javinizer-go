"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.MysqlQueryCompiler = void 0;
const default_query_compiler_js_1 = require("../../query-compiler/default-query-compiler.js");
const LITERAL_ESCAPE_REGEX = /[\\']/g;
const ID_WRAP_REGEX = /`/g;
const JSON_PATH_MEMBER_ESCAPE_REGEX = /[\\'"]/g;
class MysqlQueryCompiler extends default_query_compiler_js_1.DefaultQueryCompiler {
    getCurrentParameterPlaceholder() {
        return '?';
    }
    getLeftExplainOptionsWrapper() {
        return '';
    }
    getExplainOptionAssignment() {
        return '=';
    }
    getExplainOptionsDelimiter() {
        return ' ';
    }
    getRightExplainOptionsWrapper() {
        return '';
    }
    getLeftIdentifierWrapper() {
        return ID_WRAP_REGEX.source;
    }
    getRightIdentifierWrapper() {
        return ID_WRAP_REGEX.source;
    }
    sanitizeIdentifier(identifier) {
        return identifier.replace(ID_WRAP_REGEX, '``');
    }
    /**
     * MySQL requires escaping backslashes in string literals when using the
     * default NO_BACKSLASH_ESCAPES=OFF mode. Without this, a backslash
     * followed by a quote (\') can break out of the string literal.
     *
     * @see https://dev.mysql.com/doc/refman/9.6/en/string-literals.html
     */
    sanitizeStringLiteral(value) {
        return value.replace(LITERAL_ESCAPE_REGEX, (char) => char === '\\' ? '\\\\' : "''");
    }
    /**
     * Member values appear inside `"..."` in the JSON path, which itself sits
     * inside a SQL string literal. They must therefore be escaped twice — once
     * for the JSON path grammar, then again for MySQL's string literal parser.
     */
    sanitizeJSONPathMemberValue(value) {
        return value.replace(JSON_PATH_MEMBER_ESCAPE_REGEX, (char) => char === '\\' ? '\\\\\\\\' : char === "'" ? "''" : '\\\\"');
    }
    visitCreateIndex(node) {
        this.append('create ');
        if (node.unique) {
            this.append('unique ');
        }
        this.append('index ');
        if (node.ifNotExists) {
            this.append('if not exists ');
        }
        this.visitNode(node.name);
        if (node.using) {
            this.append(' using ');
            this.visitNode(node.using);
        }
        if (node.table) {
            this.append(' on ');
            this.visitNode(node.table);
        }
        if (node.columns) {
            this.append(' (');
            this.compileList(node.columns);
            this.append(')');
        }
        if (node.where) {
            this.append(' ');
            this.visitNode(node.where);
        }
    }
}
exports.MysqlQueryCompiler = MysqlQueryCompiler;
