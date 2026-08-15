type TrackedFs = {
    fs: typeof import("node:fs");
    readFiles: Set<string>;
    clearReadFiles: () => void;
};
type TrackedFsOptions = {
    baseDir?: string;
    fs?: typeof import("node:fs");
};
type WatchTargetOptions = {
    baseDir?: string;
    outdir?: string;
    ignoreCache?: boolean;
    includeDirectories?: boolean;
    ignorePath?: (path: string) => boolean;
};
type WatchTargets = {
    files: Set<string>;
    directories: Set<string>;
    isIgnoredPath: (path: string) => boolean;
};
export declare function isPathWithinDirectories(path: string, directories: Iterable<string>): boolean;
export declare function getWatchTargets(files: Iterable<string>, options?: WatchTargetOptions): WatchTargets;
export declare function createTrackedFs(options?: TrackedFsOptions): TrackedFs;
export {};
//# sourceMappingURL=tracked-fs.d.ts.map