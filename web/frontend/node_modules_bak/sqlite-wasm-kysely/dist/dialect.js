import { SqliteAdapter, SqliteIntrospector, SqliteQueryCompiler } from "kysely";
import { SqliteWasmDriver } from "./kysely/SqliteWasmDriver.js";
export const createDialect = (args) => {
    return {
        createAdapter: () => new SqliteAdapter(),
        createDriver: () => new SqliteWasmDriver({
            database: args.database,
        }),
        createIntrospector: (db) => new SqliteIntrospector(db),
        createQueryCompiler: () => new SqliteQueryCompiler(),
    };
};
//# sourceMappingURL=dialect.js.map