export declare class ValueHashError extends Error {
    readonly value: unknown;
    constructor(value: unknown);
}
export declare enum ByteMarker {
    Undefined = 0,
    Null = 1,
    Boolean = 2,
    Number = 3,
    String = 4,
    Object = 5,
    Array = 6,
    Date = 7,
    Uint8Array = 8,
    Symbol = 9,
    BigInt = 10
}
/** Creates a FNV1A-64 non cryptographic hash of the given value */
export declare function Hash(value: unknown): bigint;
