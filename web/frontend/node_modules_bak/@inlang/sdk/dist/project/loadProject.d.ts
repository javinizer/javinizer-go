import { type Account, type Lix } from "@lix-js/sdk";
import type { InlangPlugin } from "../plugin/schema.js";
import { type SqliteWasmDatabase } from "sqlite-wasm-kysely";
import { type PreprocessPluginBeforeImportFunction } from "../plugin/importPlugins.js";
import type { InlangProject } from "./api.js";
/**
 * Common load project logic.
 */
export declare function loadProject(args: {
    sqlite: SqliteWasmDatabase;
    lix: Lix;
    /**
     * The account that loaded the project.
     *
     * Defaults to an anonymous/new account if undefined.
     *
     * @example
     *   const account = localStorage.getItem("account")
     *   const project = await loadProject({ account })
     */
    account?: Account;
    /**
     * Provide plugins to the project.
     *
     * This is useful for testing or providing plugins that are
     * app specific. Keep in mind that provided plugins
     * are not shared with other instances.
     */
    providePlugins?: InlangPlugin[];
    /**
     * Function that preprocesses the plugin before importing it.
     *
     * The callback can be used to process plugins as needed in the
     * environment of the app. For example, Sherlock uses this to convert
     * ESM, which all inlang plugins are written in, to CJS which Sherlock
     * runs in.
     *
     * @example
     *   const project = await loadProject({ preprocessPluginBeforeImport: (moduleText) => convertEsmToCjs(moduleText) })
     *
     */
    preprocessPluginBeforeImport?: PreprocessPluginBeforeImportFunction;
}): Promise<InlangProject>;
//# sourceMappingURL=loadProject.d.ts.map