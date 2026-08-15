import type { Lix } from "@lix-js/sdk";
import type { ProjectSettings } from "../json-schema/settings.js";
import { PluginError } from "./errors.js";
import type { InlangPlugin } from "./schema.js";
/**
 * Function that preprocesses the plugin before importing it.
 *
 * - used by sherlock to convert ESM to CJS
 */
export type PreprocessPluginBeforeImportFunction = (moduleText: string) => Promise<string> | string;
export declare function importPlugins(args: {
    lix: Lix;
    settings: ProjectSettings;
    preprocessPluginBeforeImport?: PreprocessPluginBeforeImportFunction;
}): Promise<{
    plugins: InlangPlugin[];
    errors: PluginError[];
}>;
//# sourceMappingURL=importPlugins.d.ts.map