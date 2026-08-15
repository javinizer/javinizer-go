import { escapeForDoubleQuoteString } from "../services/codegen/escape.js";
const identifierPattern = /^[A-Za-z_$][A-Za-z0-9_$]*$/;
export function isValidIdentifier(name) {
    return identifierPattern.test(name);
}
export function quotePropertyKey(name) {
    return `"${escapeForDoubleQuoteString(name)}"`;
}
export function compileInputAccess(name) {
    if (isValidIdentifier(name)) {
        return `i?.${name}`;
    }
    return `i?.[${quotePropertyKey(name)}]`;
}
//# sourceMappingURL=variable-access.js.map