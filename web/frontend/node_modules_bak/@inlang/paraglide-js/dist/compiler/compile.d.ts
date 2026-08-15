import { type CompilerOptions } from "./compiler-options.js";
export type CompilationResult = {
    outputHashes: Record<string, string> | undefined;
};
/**
 * Loads, compiles, and writes the output to disk.
 *
 * This is the main function to use when you want to compile a project.
 * If you want to adjust inlang project loading, or the output, use
 * `compileProject()` instead.
 *
 * @example
 *   await compile({
 *     project: 'path/to/project',
 *     outdir: 'path/to/output',
 *   })
 */
export declare function compile(options: CompilerOptions & {
    previousCompilation?: CompilationResult;
}): Promise<CompilationResult>;
//# sourceMappingURL=compile.d.ts.map