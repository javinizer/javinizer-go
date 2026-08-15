import type { BundleNested } from "../../database/schema.js";
import type { MessageV1 } from "./schemaV1.js";
/**
 * Converts a MessageV1 into a BundleNested
 *
 * @throws If the message cannot be represented in the v1 format
 */
export declare function fromMessageV1(messageV1: MessageV1): BundleNested;
//# sourceMappingURL=fromMessageV1.d.ts.map