/**
 * Escapes some Text so that it can safely be used inside a template literal.
 */
export function escapeForTemplateLiteral(text) {
    return text
        .replace(/\\/g, "\\\\")
        .replace(/`/g, "\\`")
        .replace(/\${/g, "\\${");
}
/**
 * Escapes some Text so that it can safely be used inside a single quote string.s
 */
export function escapeForSingleQuoteString(text) {
    return text.replace(/\\/g, "\\\\").replace(/'/g, "\\'");
}
export function escapeForDoubleQuoteString(text) {
    return text.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}
//# sourceMappingURL=escape.js.map