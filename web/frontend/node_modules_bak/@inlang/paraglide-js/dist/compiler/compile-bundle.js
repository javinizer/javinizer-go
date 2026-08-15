import { compileMessage } from "./compile-message.js";
import { inputTypeForName, jsDocBundleFunctionTypes, } from "./jsdoc-types.js";
import { isValidIdentifier, quotePropertyKey } from "./variable-access.js";
import { toSafeModuleId } from "./safe-module-id.js";
import { escapeForDoubleQuoteString } from "../services/codegen/escape.js";
/**
 * Compiles all the messages in the bundle and returns an index-function + each compiled message
 */
export const compileBundle = (args) => {
    const compiledMessages = {};
    const safeBundleId = toSafeModuleId(args.bundle.id);
    const inputTypeAliasName = toBundleInputTypeAliasName(safeBundleId);
    const matchTypes = collectInputMatchTypes(args.bundle);
    const hasMarkup = bundleHasMarkup(args.bundle);
    for (const message of args.bundle.messages) {
        if (compiledMessages[message.locale]) {
            throw new Error(`Duplicate locale: ${message.locale}`);
        }
        const compiledMessage = compileMessage(args.bundle.declarations, message, message.variants, matchTypes, inputTypeAliasName);
        // set the pattern for the language tag
        compiledMessages[message.locale] = compiledMessage;
    }
    return {
        bundle: compileBundleFunction({
            bundle: args.bundle,
            availableLocales: Object.keys(args.fallbackMap),
            messageReferenceExpression: args.messageReferenceExpression,
            settings: args.settings,
            matchTypes,
            hasMarkup,
            experimentalMiddlewareLocaleSplitting: args.experimentalMiddlewareLocaleSplitting ?? false,
            inputTypeAliasName,
        }),
        messages: compiledMessages,
        matchTypes,
        inputTypeAliasName,
    };
};
const compileBundleFunction = (args) => {
    const inputs = args.bundle.declarations.filter((decl) => decl.type === "input-variable");
    const hasInputs = inputs.length > 0;
    const safeBundleId = toSafeModuleId(args.bundle.id);
    const isSafeBundleId = safeBundleId === args.bundle.id;
    const isFullyTranslated = args.availableLocales.length === args.settings?.locales.length;
    const inputType = args.inputTypeAliasName;
    const localesUnion = args.availableLocales.length === 0
        ? "never"
        : args.availableLocales.map((locale) => `"${locale}"`).join(" | ");
    const optionsType = `{ locale?: ${localesUnion} }`;
    const inputsParameterType = `${hasInputs ? "inputs" : "inputs?"}: ${inputType}`;
    const bundleFunctionType = `(${inputsParameterType}, options?: ${optionsType}) => LocalizedString`;
    const partsFunctionType = `(${inputsParameterType}, options?: ${optionsType}) => import('../runtime.js').MessagePart[]`;
    const markupSchemaType = buildMarkupSchemaType(args.bundle, args.matchTypes);
    const messageMetadataType = `import('../runtime.js').MessageMetadata<${inputType}, ${optionsType}, ${markupSchemaType}>`;
    const compileLocaleReturnStatements = (mode, continuationIndent) => args.availableLocales
        .map((locale, index) => {
        const condition = !isFullyTranslated || index < args.availableLocales.length - 1
            ? `if (locale === "${locale}") `
            : "";
        const prefix = index > 0 ? continuationIndent : "";
        const messageRef = args.messageReferenceExpression(locale, args.bundle.id);
        if (mode === "string") {
            return `${prefix}${condition}return ${messageRef}(inputs)`;
        }
        return `${prefix}${condition}return typeof ${messageRef}.parts === "function" ? ${messageRef}.parts(inputs) : [{ type: "text", value: ${messageRef}(inputs) }]`;
    })
        .join("\n");
    const englishMatchTableDoc = buildEnglishMatchTableDoc(args.bundle);
    const commonJsDoc = `/**
${englishMatchTableDoc}${jsDocBundleFunctionTypes({
        inputs,
        locales: args.availableLocales,
        matchTypes: args.matchTypes,
        inputTypeOverride: args.inputTypeAliasName,
    })}
*/`;
    const clientMiddlewareGuard = (indent) => {
        if (!args.experimentalMiddlewareLocaleSplitting) {
            return "";
        }
        return `\n${indent}if (experimentalMiddlewareLocaleSplitting && isServer === false) {\n${indent}\treturn /** @type {any} */ (globalThis).__paraglide.ssr.${safeBundleId}(inputs)\n${indent}}`;
    };
    const clientPartsMiddlewareGuard = (indent) => {
        if (!args.experimentalMiddlewareLocaleSplitting) {
            return "";
        }
        return `\n${indent}if (experimentalMiddlewareLocaleSplitting && isServer === false) {\n${indent}\tconst serverMessage = /** @type {any} */ (globalThis).__paraglide.ssr.${safeBundleId}\n${indent}\tif (typeof serverMessage.parts === "function") {\n${indent}\t\treturn /** @type {import('../runtime.js').MessagePart[]} */ (serverMessage.parts(inputs))\n${indent}\t}\n${indent}\treturn /** @type {import('../runtime.js').MessagePart[]} */ ([{ type: "text", value: serverMessage(inputs) }])\n${indent}}`;
    };
    const maybeTrackMessageCall = (indent) => {
        if (!args.experimentalMiddlewareLocaleSplitting) {
            return "";
        }
        return `\n${indent}trackMessageCall("${safeBundleId}", locale)`;
    };
    const localeResolutionStatement = (indent) => {
        if (isFullyTranslated &&
            args.availableLocales.length === 1 &&
            !args.experimentalMiddlewareLocaleSplitting) {
            return `${indent}experimentalStaticLocale ?? options.locale ?? getLocale()`;
        }
        return `${indent}const locale = experimentalStaticLocale ?? options.locale ?? getLocale()${maybeTrackMessageCall(indent)}`;
    };
    let code = "";
    if (!args.hasMarkup) {
        code = `${commonJsDoc}
${isSafeBundleId ? "export " : ""}const ${safeBundleId} = /** @type {(${bundleFunctionType}) & ${messageMetadataType}} */ ((inputs${hasInputs ? "" : " = {}"}, options = {}) => {${clientMiddlewareGuard("\t")}
${localeResolutionStatement("\t")}
	${compileLocaleReturnStatements("string", "\t")}${!isFullyTranslated
            ? `\n	return /** @type {LocalizedString} */ ("${args.bundle.id}")`
            : ""}
});`;
    }
    else {
        code = `${commonJsDoc}
${isSafeBundleId ? "export " : ""}const ${safeBundleId} = /** @type {(${bundleFunctionType}) & { parts: ${partsFunctionType} } & ${messageMetadataType}} */ (
	/* @__PURE__ */ Object.assign(
		/** @type {${bundleFunctionType}} */ ((inputs${hasInputs ? "" : " = {}"}, options = {}) => {${clientMiddlewareGuard("\t\t\t")}
${localeResolutionStatement("\t\t\t")}
			${compileLocaleReturnStatements("string", "\t\t\t")}${!isFullyTranslated
            ? `\n			return /** @type {LocalizedString} */ (${JSON.stringify(args.bundle.id)})`
            : ""}
		}),
		{
			parts: /** @type {${partsFunctionType}} */ ((inputs${hasInputs ? "" : " = {}"}, options = {}) => {${clientPartsMiddlewareGuard("\t\t\t\t")}
${localeResolutionStatement("\t\t\t\t")}
				${compileLocaleReturnStatements("parts", "\t\t\t\t")}${!isFullyTranslated
            ? `\n				return /** @type {import('../runtime.js').MessagePart[]} */ ([{ type: "text", value: ${JSON.stringify(args.bundle.id)} }])`
            : ""}
			})
		}
	)
);`;
    }
    if (isSafeBundleId === false) {
        code += `\nexport { ${safeBundleId} as "${escapeForDoubleQuoteString(args.bundle.id)}" }`;
    }
    return {
        code,
        node: args.bundle,
    };
};
function buildEnglishMatchTableDoc(bundle) {
    const message = selectEnglishMessage(bundle);
    if (!message) {
        return "";
    }
    const selectorKeys = collectSelectorKeys(message);
    const headerCells = [...selectorKeys, "output"];
    const lines = [
        `* | ${headerCells.map(escapeTableCell).join(" | ")} |`,
        `* | ${headerCells.map(() => "---").join(" | ")} |`,
    ];
    for (const variant of message.variants) {
        const rowValues = selectorKeys.map((key) => getMatchCellValue(variant, key));
        const serializedOutput = serializePatternPreview(variant.pattern);
        const outputValue = JSON.stringify(serializedOutput.length > 160
            ? `${serializedOutput.slice(0, 157)}...`
            : serializedOutput);
        lines.push(`* | ${[...rowValues, outputValue].map(escapeTableCell).join(" | ")} |`);
    }
    lines.push("*");
    return lines.join("\n");
}
function selectEnglishMessage(bundle) {
    return (bundle.messages.find((message) => message.locale === "en") ??
        bundle.messages.find((message) => message.locale.startsWith("en-")));
}
function collectSelectorKeys(message) {
    const keys = [];
    const seen = new Set();
    for (const variant of message.variants) {
        for (const match of variant.matches ?? []) {
            if (seen.has(match.key))
                continue;
            seen.add(match.key);
            keys.push(match.key);
        }
    }
    return keys;
}
function getMatchCellValue(variant, key) {
    const match = (variant.matches ?? []).find((entry) => entry.key === key);
    if (!match || match.type === "catchall-match") {
        return "*";
    }
    return JSON.stringify(match.value);
}
function escapeTableCell(value) {
    return value
        .replace(/\|/g, "\\|")
        .replace(/\r?\n/g, " ")
        .replaceAll("*/", "*\\/");
}
export function toBundleInputTypeAliasName(safeBundleId) {
    return `${toBundleTypeBaseName(safeBundleId)}Inputs`;
}
function toBundleTypeBaseName(safeBundleId) {
    let baseName = "";
    let capitalizeNext = true;
    for (const character of safeBundleId) {
        if (character === "_") {
            baseName += "_";
            capitalizeNext = true;
            continue;
        }
        if (character >= "a" && character <= "z") {
            baseName += capitalizeNext ? character.toUpperCase() : character;
        }
        else {
            baseName += character;
        }
        capitalizeNext = false;
    }
    if (!baseName) {
        baseName = "Message";
    }
    if (!/^[A-Za-z_]/.test(baseName)) {
        baseName = `Message${baseName}`;
    }
    return baseName;
}
function serializePatternPreview(pattern) {
    let result = "";
    for (const part of pattern) {
        if (part.type === "text") {
            result += part.value;
            continue;
        }
        if (part.type === "expression") {
            if (part.arg.type === "literal") {
                result += part.arg.value;
            }
            else {
                result += `{${part.arg.name}}`;
            }
        }
    }
    return result.replace(/\s+/g, " ").trim();
}
function buildMarkupSchemaType(bundle, matchTypes) {
    const tagAccumulators = new Map();
    const inputVariableNames = new Set(bundle.declarations
        .filter((declaration) => declaration.type === "input-variable")
        .map((declaration) => declaration.name));
    for (const message of bundle.messages) {
        for (const variant of message.variants) {
            for (const part of variant.pattern) {
                if (part.type !== "markup-start" && part.type !== "markup-standalone") {
                    continue;
                }
                const tag = ensureMarkupTagAccumulator(tagAccumulators, part.name);
                tag.occurrences += 1;
                if (part.type === "markup-start") {
                    tag.childrenTrue = true;
                }
                else {
                    tag.childrenFalse = true;
                }
                collectMarkupOptions(tag, part, inputVariableNames, matchTypes);
                collectMarkupAttributes(tag, part);
            }
        }
    }
    if (tagAccumulators.size === 0) {
        return "{}";
    }
    const tagEntries = Array.from(tagAccumulators.entries())
        .sort(([leftName], [rightName]) => leftName.localeCompare(rightName))
        .map(([tagName, tag]) => `${renderTypeObjectKey(tagName)}: ${renderMarkupTagType(tag)}`);
    return `{ ${tagEntries.join("; ")} }`;
}
function ensureMarkupTagAccumulator(accumulators, tagName) {
    const existing = accumulators.get(tagName);
    if (existing) {
        return existing;
    }
    const created = {
        occurrences: 0,
        childrenTrue: false,
        childrenFalse: false,
        options: new Map(),
        attributes: new Map(),
    };
    accumulators.set(tagName, created);
    return created;
}
function collectMarkupOptions(tag, part, inputVariableNames, matchTypes) {
    const seenOptions = new Set();
    for (const option of part.options ?? []) {
        const optionAccumulator = ensureMarkupValueAccumulator(tag.options, option.name);
        optionAccumulator.types.add(resolveMarkupOptionType(option.value, inputVariableNames, matchTypes));
        if (!seenOptions.has(option.name)) {
            optionAccumulator.count += 1;
            seenOptions.add(option.name);
        }
    }
}
function collectMarkupAttributes(tag, part) {
    const seenAttributes = new Set();
    for (const attribute of part.attributes ?? []) {
        const attributeAccumulator = ensureMarkupValueAccumulator(tag.attributes, attribute.name);
        attributeAccumulator.types.add(attribute.value === true ? "true" : "string");
        if (!seenAttributes.has(attribute.name)) {
            attributeAccumulator.count += 1;
            seenAttributes.add(attribute.name);
        }
    }
}
function resolveMarkupOptionType(value, inputVariableNames, matchTypes) {
    if (value.type === "literal") {
        return "string";
    }
    if (inputVariableNames.has(value.name)) {
        return inputTypeForName(value.name, matchTypes);
    }
    return "NonNullable<unknown>";
}
function ensureMarkupValueAccumulator(values, name) {
    const existing = values.get(name);
    if (existing) {
        return existing;
    }
    const created = { count: 0, types: new Set() };
    values.set(name, created);
    return created;
}
function renderMarkupTagType(tag) {
    return `{ options: ${renderMarkupObjectType(tag.options, tag.occurrences)}; attributes: ${renderMarkupObjectType(tag.attributes, tag.occurrences)}; children: ${renderChildrenType(tag)} }`;
}
function renderMarkupObjectType(values, occurrenceCount) {
    if (values.size === 0) {
        return "{}";
    }
    const properties = Array.from(values.entries())
        .sort(([leftName], [rightName]) => leftName.localeCompare(rightName))
        .map(([name, value]) => {
        const isOptional = value.count < occurrenceCount;
        return `${renderTypeObjectKey(name)}${isOptional ? "?" : ""}: ${renderTypeUnion(value.types)}`;
    });
    return `{ ${properties.join("; ")} }`;
}
function renderChildrenType(tag) {
    if (tag.childrenTrue && tag.childrenFalse) {
        return "boolean";
    }
    if (tag.childrenTrue) {
        return "true";
    }
    if (tag.childrenFalse) {
        return "false";
    }
    return "boolean";
}
function renderTypeUnion(types) {
    const sorted = Array.from(types).sort();
    if (sorted.length === 0) {
        return "never";
    }
    if (sorted.length === 1) {
        return sorted[0] ?? "never";
    }
    return sorted.join(" | ");
}
function renderTypeObjectKey(name) {
    return isValidIdentifier(name) ? name : quotePropertyKey(name);
}
function collectInputMatchTypes(bundle) {
    const inputNames = new Set(bundle.declarations
        ?.filter((decl) => decl.type === "input-variable")
        .map((decl) => decl.name) ?? []);
    const matchTypes = new Map();
    const ensureInfo = (name) => {
        const existing = matchTypes.get(name);
        if (existing)
            return existing;
        const created = { literals: new Set(), hasCatchAll: false };
        matchTypes.set(name, created);
        return created;
    };
    for (const message of bundle.messages) {
        for (const variant of message.variants) {
            if (!variant.matches || variant.matches.length === 0) {
                for (const name of inputNames) {
                    const info = ensureInfo(name);
                    info.hasCatchAll = true;
                }
                continue;
            }
            for (const match of variant.matches ?? []) {
                if (!inputNames.has(match.key))
                    continue;
                const info = ensureInfo(match.key);
                if (match.type === "catchall-match") {
                    info.hasCatchAll = true;
                    continue;
                }
                if (match.type === "literal-match") {
                    info.literals.add(match.value);
                }
            }
        }
    }
    return matchTypes;
}
function bundleHasMarkup(bundle) {
    return bundle.messages.some((message) => message.variants.some((variant) => variant.pattern.some((part) => part.type === "markup-start" ||
        part.type === "markup-end" ||
        part.type === "markup-standalone")));
}
//# sourceMappingURL=compile-bundle.js.map