import { type NewKeyValue } from "@lix-js/sdk";
import { loadProject } from "./loadProject.js";
/**
 * Load a project from a blob in memory.
 */
export declare function loadProjectInMemory(args: {
    blob: Blob;
    lixKeyValues?: NewKeyValue[];
} & Omit<Parameters<typeof loadProject>[0], "sqlite" | "lix">): Promise<import("./api.js").InlangProject>;
//# sourceMappingURL=loadProjectInMemory.d.ts.map