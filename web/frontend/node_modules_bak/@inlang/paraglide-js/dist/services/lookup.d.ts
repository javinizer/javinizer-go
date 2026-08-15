type LookupOptions = {
    locales: string[];
    baseLocale: string;
};
/**
 * Performs a lookup for the given language tag, among the available language tags,
 * according to the IETF BCP 47 spec.
 *
 * It **does not support Wildcards** at the moment.
 *
 * @see https://datatracker.ietf.org/doc/html/rfc4647#section-3.4
 */
export declare function lookup(locale: string, options: LookupOptions): string;
export {};
//# sourceMappingURL=lookup.d.ts.map