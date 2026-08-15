/**
 * Exports the content of a database as a Uint8Array.
 *
 * @example
 *   const db = createInMemoryDatabase({ readOnly: false });
 *   const content = contentFromDatabase(db);
 */
export const contentFromDatabase = (db) => {
    return db.sqlite3.capi.sqlite3_js_db_export(db);
};
//# sourceMappingURL=contentFromDatabase.js.map