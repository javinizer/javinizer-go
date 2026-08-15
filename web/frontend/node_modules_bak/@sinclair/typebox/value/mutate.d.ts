export declare class ValueMutateTypeMismatchError extends Error {
    constructor();
}
export declare class ValueMutateInvalidRootMutationError extends Error {
    constructor();
}
export type Mutable = {
    [key: string]: unknown;
} | unknown[];
/** Performs a deep mutable value assignment while retaining internal references */
export declare function Mutate(current: Mutable, next: Mutable): void;
