import type { InputVariable } from "@inlang/sdk";
export type InputMatchTypes = Map<string, {
    literals: Set<string>;
    hasCatchAll: boolean;
}>;
export declare function jsDocBundleFunctionTypes(args: {
    inputs: InputVariable[];
    locales: string[];
    matchTypes?: InputMatchTypes;
    inputTypeOverride?: string;
}): string;
/**
 * Returns the types for the input variables.
 *
 * @example
 *   const inputs = [{ name: "age" }]
 *   inputsType(inputs)
 *   >> "{ age: NonNullable<unknown> }"
 */
export declare function inputsType(inputs: InputVariable[], matchTypes?: InputMatchTypes): string;
export declare function inputTypeForName(name: string, matchTypes?: InputMatchTypes): string;
//# sourceMappingURL=jsdoc-types.d.ts.map