import { CompiledQuery, DatabaseConnection, QueryResult } from "kysely";
import { SqliteWasmDatabase } from "../util/createInMemoryDatabase.js";
export declare class SqliteWasmConnection implements DatabaseConnection {
    #private;
    constructor(db: SqliteWasmDatabase);
    executeQuery<O>(compiledQuery: CompiledQuery): Promise<QueryResult<O>>;
    streamQuery(): AsyncGenerator<never, void, unknown>;
}
