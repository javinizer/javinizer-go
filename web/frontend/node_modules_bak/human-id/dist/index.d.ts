export declare const adjectives: string[];
export declare const nouns: string[];
export declare const verbs: string[];
export declare const adverbs: string[];
export interface Options {
    separator?: string;
    capitalize?: boolean;
    adjectiveCount?: number;
    addAdverb?: boolean;
}
/**
 * Returns the human-id
 */
export declare function humanId(options?: Options | string | boolean): string;
/**
 * Returns the pool size for a set of options
 */
export declare function poolSize(options?: Options): number;
/**
 * Returns the max length for a set of options
 */
export declare function maxLength(options?: Options): number;
/**
 * Returns the min length for a set of options
 */
export declare function minLength(options?: Options): number;
export default humanId;
