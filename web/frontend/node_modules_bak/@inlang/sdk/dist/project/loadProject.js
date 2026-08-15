import { toBlob } from "@lix-js/sdk";
import { contentFromDatabase, } from "sqlite-wasm-kysely";
import { initDb } from "../database/initDb.js";
import { importPlugins, } from "../plugin/importPlugins.js";
import { withLanguageTagToLocaleMigration } from "../migrations/v2/withLanguageTagToLocaleMigration.js";
import { v4 } from "uuid";
import { importFiles } from "../import-export/importFiles.js";
import { exportFiles } from "../import-export/exportFiles.js";
/**
 * Common load project logic.
 */
export async function loadProject(args) {
    const db = initDb({ sqlite: args.sqlite });
    await maybeMigrateFirstProjectId({ lix: args.lix });
    const settingsFile = await args.lix.db
        .selectFrom("file")
        .select("data")
        .where("path", "=", "/settings.json")
        .executeTakeFirstOrThrow();
    const settings = withLanguageTagToLocaleMigration(JSON.parse(new TextDecoder().decode(settingsFile.data)));
    const importedPlugins = await importPlugins({
        settings,
        lix: args.lix,
        preprocessPluginBeforeImport: args.preprocessPluginBeforeImport,
    });
    const plugins = [...(args.providePlugins ?? []), ...importedPlugins.plugins];
    // const state = createProjectState({
    // 	...args,
    // 	settings,
    // });
    return {
        db,
        id: {
            get: async () => {
                const file = await args.lix.db
                    .selectFrom("file")
                    .where("path", "=", "/project_id")
                    .select("file.data")
                    .executeTakeFirstOrThrow();
                return new TextDecoder().decode(file.data);
            },
        },
        settings: {
            get: async () => {
                const file = await args.lix.db
                    .selectFrom("file")
                    .where("path", "=", "/settings.json")
                    .select("file.data")
                    .executeTakeFirstOrThrow();
                return withLanguageTagToLocaleMigration(JSON.parse(new TextDecoder().decode(file.data)));
            },
            set: async (newSettings) => {
                const cloned = JSON.parse(JSON.stringify(newSettings));
                cloned.languageTags = cloned.locales;
                cloned.sourceLanguageTag = cloned.baseLocale;
                await args.lix.db
                    .updateTable("file")
                    .where("path", "=", "/settings.json")
                    .set({
                    data: new TextEncoder().encode(JSON.stringify(cloned, undefined, 2)),
                })
                    .execute();
            },
        },
        plugins: {
            get: async () => plugins,
        },
        errors: {
            get: async () => [...importedPlugins.errors],
        },
        // errors: state.errors,
        importFiles: async ({ files, pluginKey }) => {
            const settingsFile = await args.lix.db
                .selectFrom("file")
                .where("path", "=", "/settings.json")
                .select("file.data")
                .executeTakeFirstOrThrow();
            const settings = JSON.parse(new TextDecoder().decode(settingsFile.data));
            return await importFiles({
                files,
                pluginKey,
                settings,
                // TODO don't use global state, might be stale
                plugins,
                db,
            });
        },
        exportFiles: async ({ pluginKey }) => {
            const settingsFile = await args.lix.db
                .selectFrom("file")
                .where("path", "=", "/settings.json")
                .select("file.data")
                .executeTakeFirstOrThrow();
            const settings = JSON.parse(new TextDecoder().decode(settingsFile.data));
            return (await exportFiles({
                pluginKey,
                db,
                settings,
                // TODO don't use global state, might be stale
                plugins,
            })).map((output) => ({ ...output, pluginKey }));
        },
        close: async () => {
            await saveDbToLix({ sqlite: args.sqlite, lix: args.lix });
            await db.destroy();
            await args.lix.db.destroy();
        },
        _sqlite: args.sqlite,
        toBlob: async () => {
            await saveDbToLix({ sqlite: args.sqlite, lix: args.lix });
            return await toBlob({ lix: args.lix });
        },
        lix: args.lix,
    };
}
async function saveDbToLix(args) {
    const data = contentFromDatabase(args.sqlite);
    await args.lix.db
        .updateTable("file")
        .set("data", data)
        .where("path", "=", "/db.sqlite")
        .execute();
}
/**
 * Old leftover migration from v1. Probably not needed anymore.
 *
 * Kept it in just in case.
 */
async function maybeMigrateFirstProjectId(args) {
    const firstProjectIdFile = await args.lix.db
        .selectFrom("file")
        .select("data")
        .where("path", "=", "/project_id")
        .executeTakeFirst();
    if (!firstProjectIdFile) {
        await args.lix.db
            .insertInto("file")
            .values({
            path: "/project_id",
            data: new TextEncoder().encode(v4()),
        })
            .execute();
    }
}
//# sourceMappingURL=loadProject.js.map