import { Database, Sqlite3Static } from "@sqlite.org/sqlite-wasm";
export type SqliteWasmDatabase = Database & {
    /**
     * The sqlite3 module used to create the database.
     *
     * Use this API to access the sqlite3 module directly.
     */
    sqlite3: Sqlite3Static;
};
export declare const createInMemoryDatabase: ({ readOnly, }: {
    readOnly?: boolean;
}) => Promise<SqliteWasmDatabase>;
