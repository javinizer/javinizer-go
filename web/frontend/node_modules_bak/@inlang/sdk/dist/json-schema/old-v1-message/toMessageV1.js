/**
 * Converts a BundleNested into a legacy format.
 *
 * @throws If the message cannot be represented in the v1 format
 */
export function toMessageV1(bundle) {
    const variants = [];
    const selectorNames = new Set();
    for (const message of bundle.messages) {
        // collect all selector names
        for (const selector of message.selectors.map((s) => ({
            type: "variable-reference",
            name: s.name,
        }))) {
            selectorNames.add(selector.name);
        }
        // collect all variants
        for (const variant of message.variants) {
            variants.push({
                languageTag: message.locale,
                match: [],
                pattern: toV1Pattern(variant.pattern),
            });
        }
    }
    const selectors = [...selectorNames].map((name) => ({
        type: "VariableReference",
        name,
    }));
    return {
        id: bundle.id,
        alias: {},
        variants,
        selectors,
    };
}
/**
 * @throws If the pattern cannot be represented in the v1 format
 */
function toV1Pattern(pattern) {
    return pattern.map((element) => {
        switch (element.type) {
            case "text": {
                return {
                    type: "Text",
                    value: element.value,
                };
            }
            case "expression": {
                if (element.arg.type === "variable-reference") {
                    return {
                        type: "VariableReference",
                        name: element.arg.name,
                    };
                }
                throw new Error(`Unsupported expression argument type`);
            }
            case "markup-start":
            case "markup-end":
            case "markup-standalone": {
                throw new Error("Markup placeholders are not supported in MessageV1 conversion");
            }
            default: {
                throw new Error(`Unsupported pattern element type`);
            }
        }
    });
}
//# sourceMappingURL=toMessageV1.js.map