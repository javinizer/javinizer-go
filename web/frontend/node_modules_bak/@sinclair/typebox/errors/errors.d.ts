import * as Types from '../typebox';
export declare enum ValueErrorType {
    ArrayContains = 0,
    ArrayMaxContains = 1,
    ArrayMaxItems = 2,
    ArrayMinContains = 3,
    ArrayMinItems = 4,
    ArrayUniqueItems = 5,
    Array = 6,
    AsyncIterator = 7,
    BigIntExclusiveMaximum = 8,
    BigIntExclusiveMinimum = 9,
    BigIntMaximum = 10,
    BigIntMinimum = 11,
    BigIntMultipleOf = 12,
    BigInt = 13,
    Boolean = 14,
    DateExclusiveMaximumTimestamp = 15,
    DateExclusiveMinimumTimestamp = 16,
    DateMaximumTimestamp = 17,
    DateMinimumTimestamp = 18,
    DateMultipleOfTimestamp = 19,
    Date = 20,
    Function = 21,
    IntegerExclusiveMaximum = 22,
    IntegerExclusiveMinimum = 23,
    IntegerMaximum = 24,
    IntegerMinimum = 25,
    IntegerMultipleOf = 26,
    Integer = 27,
    IntersectUnevaluatedProperties = 28,
    Intersect = 29,
    Iterator = 30,
    Kind = 31,
    Literal = 32,
    Never = 33,
    Not = 34,
    Null = 35,
    NumberExclusiveMaximum = 36,
    NumberExclusiveMinimum = 37,
    NumberMaximum = 38,
    NumberMinimum = 39,
    NumberMultipleOf = 40,
    Number = 41,
    ObjectAdditionalProperties = 42,
    ObjectMaxProperties = 43,
    ObjectMinProperties = 44,
    ObjectRequiredProperty = 45,
    Object = 46,
    Promise = 47,
    StringFormatUnknown = 48,
    StringFormat = 49,
    StringMaxLength = 50,
    StringMinLength = 51,
    StringPattern = 52,
    String = 53,
    Symbol = 54,
    TupleLength = 55,
    Tuple = 56,
    Uint8ArrayMaxByteLength = 57,
    Uint8ArrayMinByteLength = 58,
    Uint8Array = 59,
    Undefined = 60,
    Union = 61,
    Void = 62
}
export interface ValueError {
    type: ValueErrorType;
    schema: Types.TSchema;
    path: string;
    value: unknown;
    message: string;
}
export declare class ValueErrorsUnknownTypeError extends Types.TypeBoxError {
    readonly schema: Types.TSchema;
    constructor(schema: Types.TSchema);
}
export declare function EscapeKey(key: string): string;
export declare class ValueErrorIterator {
    private readonly iterator;
    constructor(iterator: IterableIterator<ValueError>);
    [Symbol.iterator](): IterableIterator<ValueError>;
    /** Returns the first value error or undefined if no errors */
    First(): ValueError | undefined;
}
/** Returns an iterator for each error in this value. */
export declare function Errors<T extends Types.TSchema>(schema: T, references: Types.TSchema[], value: unknown): ValueErrorIterator;
/** Returns an iterator for each error in this value. */
export declare function Errors<T extends Types.TSchema>(schema: T, value: unknown): ValueErrorIterator;
