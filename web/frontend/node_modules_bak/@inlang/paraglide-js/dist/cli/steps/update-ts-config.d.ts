import type { CliStep } from "../utils.js";
import type { Logger } from "../../services/logger/index.js";
export declare const maybeUpdateTsConfig: CliStep<{
    fs: typeof import("node:fs/promises");
    logger: Logger;
}, unknown>;
/**
 * Paraligde JS compiles to JS with JSDoc comments. TypeScript doesn't allow JS files by default.
 */
export declare const maybeUpdateTsConfigAllowJs: CliStep<{
    fs: typeof import("node:fs/promises");
    logger: Logger;
}, unknown>;
/**
 * Recursively checks whether allowJs is enabled in the provided tsconfig or any
 * referenced configuration files.
 *
 * @param tsconfigPath The path to the tsconfig to inspect.
 * @param fs The file system used to read the configs.
 * @param visited A set of already inspected files to avoid circular lookups.
 * @example
 * ```ts
 * await hasAllowJsEnabled("./tsconfig.json", fs);
 * ```
 */
export declare const hasAllowJsEnabled: (tsconfigPath: string, fs: typeof import("node:fs/promises"), visited?: Set<string>) => Promise<boolean>;
//# sourceMappingURL=update-ts-config.d.ts.map