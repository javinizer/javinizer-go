/// <reference types="./sqlite-query-compiler.d.ts" />
import { DefaultQueryCompiler } from '../../query-compiler/default-query-compiler.js';
const ID_WRAP_REGEX = /"/g;
const JSON_PATH_MEMBER_ESCAPE_REGEX = /[\\'"]/g;
export class SqliteQueryCompiler extends DefaultQueryCompiler {
    visitOrAction(node) {
        this.append('or ');
        this.append(node.action);
    }
    getCurrentParameterPlaceholder() {
        return '?';
    }
    getLeftExplainOptionsWrapper() {
        return '';
    }
    getRightExplainOptionsWrapper() {
        return '';
    }
    getLeftIdentifierWrapper() {
        return '"';
    }
    getRightIdentifierWrapper() {
        return '"';
    }
    getAutoIncrement() {
        return 'autoincrement';
    }
    sanitizeIdentifier(identifier) {
        return identifier.replace(ID_WRAP_REGEX, '""');
    }
    sanitizeJSONPathMemberValue(value) {
        return value.replace(JSON_PATH_MEMBER_ESCAPE_REGEX, (char) => char === '\\' ? '\\\\' : char === "'" ? "''" : '\\"');
    }
    visitDefaultInsertValue(_) {
        // sqlite doesn't support the `default` keyword in inserts.
        this.append('null');
    }
}
