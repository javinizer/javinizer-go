import type { Kysely } from "kysely";
import type { InlangDatabaseSchema, NewBundleNested } from "../database/schema.js";
export declare const upsertBundleNested: (db: Kysely<InlangDatabaseSchema>, bundle: NewBundleNested) => Promise<void>;
//# sourceMappingURL=upsertBundleNested.d.ts.map