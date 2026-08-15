import { SqliteWasmDatabase } from "./createInMemoryDatabase.js";
export declare const importDatabase: ({ db, content, schema, readOnly, }: {
    db: SqliteWasmDatabase;
    content: Uint8Array;
    schema?: string;
    readOnly?: boolean;
}) => SqliteWasmDatabase;
