import type { Lix } from "@lix-js/sdk";
/**
 * Implements a "Network-First" caching strategy.
 */
export declare function withCache(moduleLoader: (uri: string) => Promise<string>, lix: Lix): (uri: string) => Promise<string>;
//# sourceMappingURL=cache.d.ts.map