import { DEFAULT_OUTDIR } from "../defaults.js";
import { prompt } from "../utils.js";
export const promptForOutdir = async (ctx) => {
    const response = await prompt("Which outdir should Paraglide write generated files to?", {
        type: "text",
        initial: DEFAULT_OUTDIR,
        default: DEFAULT_OUTDIR,
        placeholder: "Relative path from the package root to the desired compiler output directory",
    });
    if (!response.startsWith("./")) {
        ctx.logger.warn("You must enter a valid relative path starting from the package root.");
        return await promptForOutdir(ctx);
    }
    return {
        ...ctx,
        outdir: response,
    };
};
//# sourceMappingURL=prompt-for-outdir.js.map