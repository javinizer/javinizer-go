import { PluginError } from "@inlang/sdk";
export declare function classifyProjectErrors(errors: readonly Error[]): {
    fatalErrors: Error[];
    nonFatalErrors: PluginError[];
};
/**
 * Splits an array into two arrays based on the predicate
 */
export declare function split<T, U extends T>(array: T[], predicate: (value: T) => value is U): [U[], T[]];
//# sourceMappingURL=error-handling.d.ts.map