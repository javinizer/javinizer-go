import { loadProjectInMemory } from "./loadProjectInMemory.js";
import { type Lix } from "@lix-js/sdk";
import fs from "node:fs";
import type { InlangPlugin } from "../plugin/schema.js";
import type { ProjectSettings } from "../json-schema/settings.js";
import type { ImportFile } from "./api.js";
/**
 * Loads a project from a directory.
 *
 * Main use case are dev tools that want to load a project from a directory
 * that is stored in git.
 *
 * @example
 *   const project = await loadProjectFromDirectory({
 *     path: "./project.inlang",
 *     fs: await import("node:fs"),
 *   });
 */
export declare function loadProjectFromDirectory(args: {
    /**
     * The path to the inlang project directory.
     */
    path: string;
    /**
     * The file system module to use for reading and writing files.
     */
    fs: typeof fs;
    /**
     * The interval in milliseconds at which to sync the project with the file system.
     *
     * If not provided, syncing only happens once on load.
     */
    syncInterval?: number;
} & Omit<Parameters<typeof loadProjectInMemory>[0], "blob">): Promise<{
    errors: {
        get: () => Promise<Error[]>;
    };
    db: import("kysely").Kysely<import("../index.js").InlangDatabaseSchema>;
    _sqlite: import("sqlite-wasm-kysely").SqliteWasmDatabase;
    id: {
        get: () => Promise<string>;
    };
    plugins: {
        get: () => Promise<readonly InlangPlugin[]>;
    };
    settings: {
        get: () => Promise<ProjectSettings>;
        set: (settings: ProjectSettings) => Promise<void>;
    };
    lix: Lix;
    importFiles: (args: {
        pluginKey: InlangPlugin["key"];
        files: ImportFile[];
    }) => Promise<void>;
    exportFiles: (args: {
        pluginKey: InlangPlugin["key"];
    }) => Promise<import("./api.js").ExportFile[]>;
    close: () => Promise<void>;
    toBlob: () => Promise<Blob>;
}>;
export declare class WarningDeprecatedLintRule extends Error {
    constructor(module: string);
}
export declare class ResourceFileImportError extends Error {
    path: string;
    constructor(args: {
        cause: Error;
        path: string;
    });
}
//# sourceMappingURL=loadProjectFromDirectory.d.ts.map