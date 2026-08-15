import { detectJsonFormatting } from "../../utilities/detect-json-formatting.js";
export function updatePackageJson(opt) {
    return async (ctx) => {
        const file = await ctx.fs.readFile(ctx.packageJsonPath, {
            encoding: "utf-8",
        });
        const stringify = detectJsonFormatting(file);
        let pkg;
        try {
            pkg = JSON.parse(file);
            if (typeof pkg !== "object" || pkg === null) {
                throw new Error();
            }
        }
        catch {
            ctx.logger.error(`Your ./package.json does not contain valid JSON. Please fix it and try again.`);
            process.exit(1);
        }
        try {
            if (opt.dependencies)
                pkg.dependencies = await opt.dependencies(pkg.dependencies || {});
            if (opt.devDependencies)
                pkg.devDependencies = await opt.devDependencies(pkg.devDependencies || {});
            if (opt.scripts)
                pkg.scripts = await opt.scripts(pkg.scripts || {});
        }
        catch {
            return ctx;
        }
        await ctx.fs.writeFile("./package.json", stringify(pkg));
        return ctx;
    };
}
//# sourceMappingURL=update-package-json.js.map