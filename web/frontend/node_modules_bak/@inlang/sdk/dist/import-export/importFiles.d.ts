import type { Kysely } from "kysely";
import type { ProjectSettings } from "../json-schema/settings.js";
import type { InlangDatabaseSchema } from "../database/schema.js";
import type { InlangPlugin } from "../plugin/schema.js";
import type { ImportFile } from "../project/api.js";
export declare function importFiles(args: {
    files: ImportFile[];
    readonly pluginKey: string;
    readonly settings: ProjectSettings;
    readonly plugins: readonly InlangPlugin[];
    readonly db: Kysely<InlangDatabaseSchema>;
}): Promise<void>;
//# sourceMappingURL=importFiles.d.ts.map