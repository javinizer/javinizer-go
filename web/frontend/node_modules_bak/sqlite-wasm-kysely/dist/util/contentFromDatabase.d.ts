import { SqliteWasmDatabase } from "./createInMemoryDatabase.js";
/**
 * Exports the content of a database as a Uint8Array.
 *
 * @example
 *   const db = createInMemoryDatabase({ readOnly: false });
 *   const content = contentFromDatabase(db);
 */
export declare const contentFromDatabase: (db: SqliteWasmDatabase) => Uint8Array;
