import type { LocalVariable } from "@inlang/sdk";
/**
 * Compiles a local variable.
 *
 * @example
 *   const code = compileLocalVariable({
 *    type: "local-variable",
 *    name: "myVar",
 *    value: { type: "literal", value: "Hello" }
 *   });
 *   >> code === "const myVar = 'Hello';"
 */
export declare function compileLocalVariable(args: {
    locale: string;
    declaration: LocalVariable;
}): string;
//# sourceMappingURL=compile-local-variable.d.ts.map