import consola from "consola";
/**
 * Wrapper to exit the process if the user presses CTRL+C.
 */
export const prompt = async (message, options) => {
    const response = await consola.prompt(message, options);
    if (response?.toString() === "Symbol(clack:cancel)") {
        process.exit(0);
    }
    return response;
};
export const promptSelection = async (message, options = {
    options: [],
}) => {
    return prompt(message, {
        type: "select",
        ...options,
    });
};
//# sourceMappingURL=utils.js.map