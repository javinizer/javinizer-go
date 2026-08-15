import { type KyselyPlugin, type PluginTransformQueryArgs, type PluginTransformResultArgs, type QueryResult, type RootOperationNode, type UnknownRow } from "kysely";
import type { SqliteWasmDatabase } from "sqlite-wasm-kysely";
export declare class JsonbPlugin implements KyselyPlugin {
    #private;
    constructor(args: {
        database: SqliteWasmDatabase;
    });
    /**
     * For an outgoing query like insert or update, the JSON
     * values are transformed into `jsonb` function calls when
     * executed against the database.
     */
    transformQuery(args: PluginTransformQueryArgs): RootOperationNode;
    /**
     * For incoming query results, the JSON binaries are parsed
     * into JSON objects.
     */
    transformResult(args: PluginTransformResultArgs): Promise<QueryResult<UnknownRow>>;
}
//# sourceMappingURL=jsonbPlugin.d.ts.map