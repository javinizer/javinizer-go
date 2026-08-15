export const addVitePlugin = async (ctx) => {
    try {
        const fileContent = await ctx.fs.readFile(ctx.configPath, {
            encoding: "utf-8",
        });
        const result = updateViteConfig({
            config: fileContent,
            projectPath: ctx.projectPath,
            outdir: ctx.outdir,
        });
        if (result.ok) {
            await ctx.fs.writeFile(ctx.configPath, result.updated);
            ctx.logger?.success("Added the Paraglide JS vite plugin to the vite config.");
        }
        else {
            const msg = [
                "Failed to add the Paraglide JS vite plugin in vite.config.js.",
                "Reason: " + result.reason,
                "",
                "Please add the plugin manually.",
            ].join("\n");
            ctx.logger?.warn(msg);
            return ctx;
        }
    }
    catch {
        ctx.logger?.error(`Failed to add the Paraglide JS vite plugin to ${ctx.configPath}.`);
        process.exit(1);
    }
    return ctx;
};
/**
 * @private Only exported for testings
 */
function updateViteConfig(args) {
    if (args.config.includes("@inlang/paraglide-js")) {
        return {
            ok: false,
            reason: "Already present",
        };
    }
    const PLUGINS_REGEX = /plugins\s*:\s*\[/g;
    const match = PLUGINS_REGEX.exec(args.config);
    if (!match) {
        return {
            ok: false,
            reason: "Could not find the plugins array",
        };
    }
    const endIndex = match.index + match[0].length;
    const before = args.config.slice(0, endIndex);
    const after = args.config.slice(endIndex);
    const updatedConfig = "import { paraglideVitePlugin } from '@inlang/paraglide-js'\n" +
        before +
        `paraglideVitePlugin({ project: '${args.projectPath}', outdir: '${args.outdir}' }),` +
        after;
    return {
        ok: true,
        updated: updatedConfig,
    };
}
//# sourceMappingURL=add-vite-plugin.js.map