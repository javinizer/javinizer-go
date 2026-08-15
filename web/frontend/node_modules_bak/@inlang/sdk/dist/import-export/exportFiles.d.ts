import type { Kysely } from "kysely";
import type { ProjectSettings } from "../json-schema/settings.js";
import type { InlangDatabaseSchema } from "../database/schema.js";
import type { InlangPlugin } from "../plugin/schema.js";
export declare function exportFiles(opts: {
    readonly pluginKey: string;
    readonly settings: ProjectSettings;
    readonly plugins: readonly InlangPlugin[];
    readonly db: Kysely<InlangDatabaseSchema>;
}): Promise<import("../index.js").ExportFile[]>;
//# sourceMappingURL=exportFiles.d.ts.map