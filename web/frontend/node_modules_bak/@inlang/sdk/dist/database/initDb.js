import { CamelCasePlugin, Kysely } from "kysely";
import { applySchema } from "./schema.js";
import { createDialect } from "sqlite-wasm-kysely";
import { v7 } from "uuid";
import { humanId } from "../human-id/human-id.js";
import { JsonbPlugin } from "./jsonbPlugin.js";
export function initDb(args) {
    initDefaultValueFunctions({ sqlite: args.sqlite });
    applySchema({ sqlite: args.sqlite });
    const db = new Kysely({
        dialect: createDialect({
            database: args.sqlite,
        }),
        plugins: [
            new CamelCasePlugin(),
            new JsonbPlugin({ database: args.sqlite }),
        ],
    });
    return db;
}
function initDefaultValueFunctions(args) {
    args.sqlite.createFunction({
        name: "uuid_v7",
        arity: 0,
        xFunc: () => v7(),
    });
    args.sqlite.createFunction({
        name: "human_id",
        arity: 0,
        xFunc: () => humanId(),
    });
}
//# sourceMappingURL=initDb.js.map