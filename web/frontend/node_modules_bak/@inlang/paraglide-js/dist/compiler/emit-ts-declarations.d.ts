/**
 * Generates `.d.ts` files for the compiled Paraglide output using the TypeScript compiler.
 *
 * @param output - The generated compiler output keyed by relative file path.
 * @returns The generated declaration files keyed by relative path.
 *
 * @example
 * const declarations = await emitTsDeclarations(output);
 * // Merge them into the compiler output before writing to disk
 */
export declare function emitTsDeclarations(output: Record<string, string>): Promise<Record<string, string>>;
//# sourceMappingURL=emit-ts-declarations.d.ts.map