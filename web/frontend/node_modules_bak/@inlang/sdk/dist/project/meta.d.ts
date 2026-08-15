import type fs from "node:fs/promises";
export type ProjectMeta = {
    highestSdkVersion?: string;
};
export declare function readProjectMeta(args: {
    fs: typeof fs;
    projectPath: string;
}): Promise<ProjectMeta | undefined>;
export declare function pickHighestVersion(versions: Array<string | undefined>): string | undefined;
export declare function compareSemver(left: string, right: string): number | null;
//# sourceMappingURL=meta.d.ts.map