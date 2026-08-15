"use strict";
var __createBinding = (this && this.__createBinding) || (Object.create ? (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    var desc = Object.getOwnPropertyDescriptor(m, k);
    if (!desc || ("get" in desc ? !m.__esModule : desc.writable || desc.configurable)) {
      desc = { enumerable: true, get: function() { return m[k]; } };
    }
    Object.defineProperty(o, k2, desc);
}) : (function(o, m, k, k2) {
    if (k2 === undefined) k2 = k;
    o[k2] = m[k];
}));
var __setModuleDefault = (this && this.__setModuleDefault) || (Object.create ? (function(o, v) {
    Object.defineProperty(o, "default", { enumerable: true, value: v });
}) : function(o, v) {
    o["default"] = v;
});
var __importStar = (this && this.__importStar) || (function () {
    var ownKeys = function(o) {
        ownKeys = Object.getOwnPropertyNames || function (o) {
            var ar = [];
            for (var k in o) if (Object.prototype.hasOwnProperty.call(o, k)) ar[ar.length] = k;
            return ar;
        };
        return ownKeys(o);
    };
    return function (mod) {
        if (mod && mod.__esModule) return mod;
        var result = {};
        if (mod != null) for (var k = ownKeys(mod), i = 0; i < k.length; i++) if (k[i] !== "default") __createBinding(result, mod, k[i]);
        __setModuleDefault(result, mod);
        return result;
    };
})();
Object.defineProperty(exports, "__esModule", { value: true });
exports.FileMigrationProvider = void 0;
const object_utils_js_1 = require("../util/object-utils.js");
/**
 * Reads all migrations from a folder in node.js.
 *
 * ### Examples
 *
 * ```ts
 * import { promises as fs } from 'node:fs'
 * import path from 'node:path'
 *
 * new FileMigrationProvider({
 *   fs,
 *   path,
 *   migrationFolder: 'path/to/migrations/folder'
 * })
 * ```
 */
class FileMigrationProvider {
    #props;
    constructor(props) {
        this.#props = props;
    }
    async getMigrations() {
        const migrations = {};
        const files = await this.#props.fs.readdir(this.#props.migrationFolder);
        for (const fileName of files) {
            if (fileName.endsWith('.js') ||
                (fileName.endsWith('.ts') && !fileName.endsWith('.d.ts')) ||
                fileName.endsWith('.mjs') ||
                (fileName.endsWith('.mts') && !fileName.endsWith('.d.mts'))) {
                const migration = await Promise.resolve(`${
                /* webpackIgnore: true */ this.#props.path.join(this.#props.migrationFolder, fileName)}`).then(s => __importStar(require(s)));
                const migrationKey = fileName.substring(0, fileName.lastIndexOf('.'));
                // Handle esModuleInterop export's `default` prop...
                if (isMigration(migration?.default)) {
                    migrations[migrationKey] = migration.default;
                }
                else if (isMigration(migration)) {
                    migrations[migrationKey] = migration;
                }
            }
        }
        return migrations;
    }
}
exports.FileMigrationProvider = FileMigrationProvider;
function isMigration(obj) {
    return (0, object_utils_js_1.isObject)(obj) && (0, object_utils_js_1.isFunction)(obj.up);
}
