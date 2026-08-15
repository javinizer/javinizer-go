import path from "node:path";
const windowsSlashRE = /\\/g;
function slash(p) {
    return p.replace(windowsSlashRE, "/");
}
const isWindows = typeof process !== "undefined" && process.platform === "win32";
export function nodeNormalizePath(id) {
    return path.posix.normalize(isWindows ? slash(id) : id);
}
//# sourceMappingURL=node-normalize-path.js.map