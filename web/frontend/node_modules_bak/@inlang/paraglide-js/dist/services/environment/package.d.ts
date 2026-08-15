/**
 * Attempts to find the package.json file that's closest to the current working directory.
 *
 * @param fs The filesystem to use.
 * @param cwd The current working directory.
 * @returns The path to the package.json file, or undefined if none was found.
 */
export declare function findPackageJson(fs: typeof import("node:fs/promises"), cwd: string): Promise<string | undefined>;
export declare function getPackageJson(fs: typeof import("node:fs/promises"), cwd: string): Promise<unknown | undefined>;
//# sourceMappingURL=package.d.ts.map