import type { Kysely } from "kysely";
import type { BundleNestedUpdate, InlangDatabaseSchema } from "../database/schema.js";
export declare const updateBundleNested: (db: Kysely<InlangDatabaseSchema>, bundle: BundleNestedUpdate & {
    id: string;
    messages: {
        id: string;
        variants: {
            id: string;
        }[];
    }[];
}) => Promise<void>;
//# sourceMappingURL=updateBundleNested.d.ts.map