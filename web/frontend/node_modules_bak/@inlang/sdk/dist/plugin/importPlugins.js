import { PluginError, PluginImportError } from "./errors.js";
import { withCache } from "./cache.js";
export async function importPlugins(args) {
    const plugins = [];
    const errors = [];
    for (const uri of args.settings.modules ?? []) {
        try {
            let moduleAsText = await withCache(fetchPlugin, args.lix)(uri);
            if (args.preprocessPluginBeforeImport) {
                moduleAsText = await args.preprocessPluginBeforeImport(moduleAsText);
            }
            let moduleAsURL;
            if (process.versions.bun) {
                // In bun we need to do dynamic imports differently
                moduleAsURL = URL.createObjectURL(new Blob([moduleAsText], { type: "text/javascript" }));
            }
            else {
                // node and others
                moduleAsURL = "data:text/javascript;base64," + btoa(moduleAsText);
            }
            const { default: module } = await import(/* @vite-ignore */ moduleAsURL);
            // old legacy message lint rules are not supported
            // and ingored for backwards compatibility
            if (module.id?.includes("messageLintRule")) {
                continue;
            }
            plugins.push(module);
        }
        catch (e) {
            errors.push(new PluginImportError({ plugin: uri, cause: e }));
        }
    }
    return { plugins, errors };
}
async function fetchPlugin(uri) {
    try {
        const response = await fetch(uri);
        return await response.text();
    }
    catch (error) {
        throw new PluginImportError({
            plugin: uri,
            cause: error,
        });
    }
}
//# sourceMappingURL=importPlugins.js.map