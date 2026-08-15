import { DatabaseConnection, Driver } from "kysely";
import { SqliteWasmDialectConfig } from "./SqliteWasmDialectConfig.js";
export declare class SqliteWasmDriver implements Driver {
    #private;
    constructor(config: SqliteWasmDialectConfig);
    init(): Promise<void>;
    acquireConnection(): Promise<DatabaseConnection>;
    beginTransaction(connection: DatabaseConnection): Promise<void>;
    commitTransaction(connection: DatabaseConnection): Promise<void>;
    rollbackTransaction(connection: DatabaseConnection): Promise<void>;
    releaseConnection(): Promise<void>;
    destroy(): Promise<void>;
}
