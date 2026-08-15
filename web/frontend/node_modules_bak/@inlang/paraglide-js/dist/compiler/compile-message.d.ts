import type { Declaration, Message, Variant } from "@inlang/sdk";
import type { Compiled } from "./types.js";
import { type InputMatchTypes } from "./jsdoc-types.js";
/**
 * Returns the compiled message as a string
 *
 */
export declare const compileMessage: (declarations: Declaration[], message: Message, variants: Variant[], matchTypes?: InputMatchTypes, inputTypeAliasName?: string) => Compiled<Message>;
//# sourceMappingURL=compile-message.d.ts.map