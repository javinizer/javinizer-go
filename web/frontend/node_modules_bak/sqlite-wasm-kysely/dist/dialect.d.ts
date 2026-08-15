import { SqliteAdapter, SqliteIntrospector, SqliteQueryCompiler } from "kysely";
import { SqliteWasmDriver } from "./kysely/SqliteWasmDriver.js";
import { SqliteWasmDatabase } from "./util/createInMemoryDatabase.js";
export declare const createDialect: (args: {
    database: SqliteWasmDatabase;
}) => {
    createAdapter: () => SqliteAdapter;
    createDriver: () => SqliteWasmDriver;
    createIntrospector: (db: any) => SqliteIntrospector;
    createQueryCompiler: () => SqliteQueryCompiler;
};
