import { PluginDoesNotImplementFunctionError, PluginMissingError, } from "../plugin/errors.js";
export async function exportFiles(opts) {
    const plugin = opts.plugins.find((p) => p.key === opts.pluginKey);
    if (!plugin)
        throw new PluginMissingError({ plugin: opts.pluginKey });
    if (!plugin.exportFiles) {
        throw new PluginDoesNotImplementFunctionError({
            plugin: opts.pluginKey,
            function: "exportFiles",
        });
    }
    const bundles = await opts.db.selectFrom("bundle").selectAll().execute();
    const messages = await opts.db.selectFrom("message").selectAll().execute();
    const variants = await opts.db.selectFrom("variant").selectAll().execute();
    const files = await plugin.exportFiles({
        settings: structuredClone(opts.settings),
        bundles,
        messages,
        variants,
    });
    return files;
}
//# sourceMappingURL=exportFiles.js.map