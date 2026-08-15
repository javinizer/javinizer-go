import type { NodeFsPromisesSubsetLegacy } from "../plugin/schema.js";
/**
 * Functions from a path like `./local-plugins/mock-plugin.js` need to be able
 * to be called with relative paths, even if their implementation expects absolute ones.
 *
 * This mapping is required for backwards compatibility.
 * Relative paths in the project.inlang/settings.json
 * file are resolved to absolute paths with `*.inlang`
 * being pruned.
 *
 * @example
 *   "/website/project.inlang"
 *   "./local-plugins/mock-plugin.js"
 *   -> "/website/local-plugins/mock-plugin.js"
 *
 */
export declare function withAbsolutePaths(fs: NodeFsPromisesSubsetLegacy, projectPath: string): NodeFsPromisesSubsetLegacy;
/**
 * Joins a path from a project path.
 *
 * @example
 *   absolutePathFromProject("/project.inlang", "./local-plugins/mock-plugin.js") -> "/local-plugins/mock-plugin.js"
 *
 *   absolutePathFromProject("/website/project.inlang", "./mock-plugin.js") -> "/website/mock-plugin.js"
 */
export declare function absolutePathFromProject(projectPath: string, filePath: string): string;
//# sourceMappingURL=path-helpers.d.ts.map