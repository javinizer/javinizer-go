import { PluginError } from "@inlang/sdk";
export function classifyProjectErrors(errors) {
    const isModuleError = (err) => err instanceof PluginError;
    const [moduleErrors, otherErrors] = split(errors, isModuleError);
    const isFatalModuleError = (err) => err.plugin.includes("plugin");
    const [fatalModuleErrors, nonFatalModuleErrors] = split(moduleErrors, isFatalModuleError);
    const fatalErrors = [...fatalModuleErrors, ...otherErrors];
    const nonFatalErrors = [...nonFatalModuleErrors];
    return { fatalErrors, nonFatalErrors };
}
/**
 * Splits an array into two arrays based on the predicate
 */
export function split(array, predicate) {
    const mask = array.map(predicate);
    const result = array.filter((_, index) => mask[index]);
    const rest = array.filter((_, index) => !mask[index]);
    return [result, rest];
}
//# sourceMappingURL=error-handling.js.map