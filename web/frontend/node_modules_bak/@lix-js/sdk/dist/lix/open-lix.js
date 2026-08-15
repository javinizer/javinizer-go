import {} from "sqlite-wasm-kysely";
import { initDb } from "../database/init-db.js";
import { initFileQueueProcess } from "../file-queue/file-queue-process.js";
import { sql } from "kysely";
import { initSyncProcess } from "../sync/sync-process.js";
/**
 * Common setup between different lix environments.
 */
export async function openLix(args) {
    const db = initDb({ sqlite: args.database });
    if (args.keyValues && args.keyValues.length > 0) {
        await db
            .insertInto("key_value")
            .values(args.keyValues)
            .onConflict((oc) => oc.doUpdateSet((eb) => ({ value: eb.ref("excluded.value") })))
            .execute();
    }
    if (args.account) {
        await db.transaction().execute(async (trx) => {
            // delete the default inserted active account from `initDb`
            await trx.deleteFrom("active_account").execute();
            await trx
                .insertInto("active_account")
                .values(args.account)
                .onConflict((oc) => oc.doUpdateSet(() => ({ ...args.account })))
                .execute();
        });
    }
    const plugins = [];
    if (args.providePlugins && args.providePlugins.length > 0) {
        plugins.push(...args.providePlugins);
    }
    const plugin = {
        getAll: async () => plugins,
    };
    await initFileQueueProcess({ lix: { db, plugin, sqlite: args.database } });
    await initSyncProcess({ lix: { db, plugin, sqlite: args.database } });
    return {
        db,
        sqlite: args.database,
        plugin,
    };
}
/**
 * Get all used file extensions.
 */
export async function usedFileExtensions(db) {
    const result = await sql `
	WITH RECURSIVE numbers(i) AS (
		SELECT 1
		UNION ALL
		SELECT i + 1 FROM numbers WHERE i < 1000 -- Limit to 1000 characters for path length
	),
	REVERSED AS (
		SELECT id,
					GROUP_CONCAT(SUBSTR(path, LENGTH(path) - i + 1, 1), '') AS reversed_path
		FROM file, numbers
		WHERE i <= LENGTH(path)
		GROUP BY id, path
	),
	EXTENSIONS AS (
		SELECT DISTINCT SUBSTR(path, LENGTH(path) - INSTR(reversed_path, '.') + 2) AS extension
		FROM file
		JOIN REVERSED ON file.id = REVERSED.id
		WHERE INSTR(reversed_path, '.') > 0
	)
	SELECT extension FROM EXTENSIONS;
	`.execute(db);
    return result.rows.map((row) => row.extension);
}
//# sourceMappingURL=open-lix.js.map
