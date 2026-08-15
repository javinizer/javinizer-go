/**
 * Detects the formatting of a JSON file and returns a function
 * that can be used to stringify JSON with the same formatting.
 *
 * @example
 *   const file = await fs.readFile("./messages.json", { encoding: "utf-8" })
 *   const stringify = detectJsonFormatting(file)
 *   const newFile = stringify(json)
 */
export declare const detectJsonFormatting: (file: string) => ((value: Parameters<typeof JSON.stringify>[0], replacer?: Parameters<typeof JSON.stringify>[1]) => string);
//# sourceMappingURL=detectJsonFormatting.d.ts.map