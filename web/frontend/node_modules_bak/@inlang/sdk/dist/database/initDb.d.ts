import { Kysely } from "kysely";
import { type InlangDatabaseSchema } from "./schema.js";
import { type SqliteWasmDatabase } from "sqlite-wasm-kysely";
export declare function initDb(args: {
    sqlite: SqliteWasmDatabase;
}): Kysely<InlangDatabaseSchema>;
//# sourceMappingURL=initDb.d.ts.map