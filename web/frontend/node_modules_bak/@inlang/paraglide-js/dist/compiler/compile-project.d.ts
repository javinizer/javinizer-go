import { type InlangProject } from "@inlang/sdk";
import { type CompilerOptions } from "./compiler-options.js";
/**
 * Takes an inlang project and compiles it into a set of files.
 *
 * Use this function for more programmatic control than `compile()`.
 * You can adjust the output structure and get the compiled files as a return value.
 *
 * @example
 *   const output = await compileProject({ project, projectPath: "./project.inlang" });
 *   await writeOutput('path', output, fs.promises);
 */
export declare const compileProject: (args: {
    project: InlangProject;
    projectPath?: string;
    compilerOptions?: Omit<CompilerOptions, "fs" | "project" | "outdir">;
}) => Promise<Record<string, string>>;
export declare function getFallbackMap<T extends string>(locales: T[], baseLocale: NoInfer<T>): Record<T, T | undefined>;
//# sourceMappingURL=compile-project.d.ts.map