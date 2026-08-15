import type { BundleNested } from "../../database/schema.js";
import type { MessageV1 } from "./schemaV1.js";
/**
 * Converts a BundleNested into a legacy format.
 *
 * @throws If the message cannot be represented in the v1 format
 */
export declare function toMessageV1(bundle: BundleNested): MessageV1;
//# sourceMappingURL=toMessageV1.d.ts.map