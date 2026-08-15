import type { ProjectSettings } from "./json-schema/settings.js";
import type { Match, NewBundleNested, NewMessageNested, Variant } from "./database/schema.js";
import type { Text } from "./json-schema/pattern.js";
/**
 * create v2 Bundle with a random human ID
 *
 * @deprecated
 *
 * use the database directly
 *
 * - less code because the database has default values
 * - `createMessage` is misleading because it does not treat expressions in the text
 *
 * @example createBundle({
 *   messages: [
 * 		 createMessage({locale: "en", text: "Hello world!"})
 * 		 createMessage({locale: "de", text: "Hallo Welt!"})
 *   ]
 * })
 */
export declare function createBundle(args: {
    id?: string;
    messages: NewMessageNested[];
}): NewBundleNested;
/**
 *
 * @deprecated
 * use the database directly
 *
 * - text will always be a string, no matter
 *   if an expression is provided like hello "{username}"
 * - the database has default values
 *
 * ```
 * await project.db.insertInto("message").values({
 * 		bundleId: "bundleId",
 *    pattern: []
 * 		...
 * })
 * ```
 *
 * create v2 Messsage AST with a randomId, and text-only pattern
 * @example createMessage({locale: "en", text: "Hello world"})
 */
export declare function createMessage(args: {
    bundleId: string;
    locale: ProjectSettings["locales"][number];
    text: string;
    matches?: Match[];
}): NewMessageNested;
/**
 *
 * @deprecated
 *
 * use the database directly
 *
 * - less code because the database has default values
 * - `text` is misleading because it does not treat expressions in the text
 *
 * create v2 Variant AST with text-only pattern
 * @example createVariant({match: ["*"], text: "Hello world"})
 */
export declare function createVariant(args: {
    messageId: string;
    id?: string;
    text?: string;
    matches?: Match[];
    pattern?: Variant["pattern"];
}): Variant;
export declare function toTextElement(text: string): Text;
//# sourceMappingURL=helper.d.ts.map