export type ObjectType = Record<PropertyKey, unknown>;
export type ArrayType = unknown[];
export type ValueType = null | undefined | symbol | bigint | number | boolean | string;
export type TypedArrayType = Int8Array | Uint8Array | Uint8ClampedArray | Int16Array | Uint16Array | Int32Array | Uint32Array | Float32Array | Float64Array | BigInt64Array | BigUint64Array;
/** Returns true if this value is an async iterator */
export declare function IsAsyncIterator(value: unknown): value is AsyncIterableIterator<any>;
/** Returns true if this value is an iterator */
export declare function IsIterator(value: unknown): value is IterableIterator<any>;
/** Returns true if this value is a typed array */
export declare function IsTypedArray(value: unknown): value is TypedArrayType;
/** Returns true if this value is a Promise */
export declare function IsPromise(value: unknown): value is Promise<unknown>;
/** Returns true if the value is a Uint8Array */
export declare function IsUint8Array(value: unknown): value is Uint8Array;
/** Returns true if this value is a Date */
export declare function IsDate(value: unknown): value is Date;
/** Returns true if this value has this property key */
export declare function HasPropertyKey<K extends PropertyKey>(value: Record<any, unknown>, key: K): value is ObjectType & Record<K, unknown>;
/** Returns true if this object is not an instance of any other type */
export declare function IsPlainObject(value: unknown): value is ObjectType;
/** Returns true of this value is an object type */
export declare function IsObject(value: unknown): value is ObjectType;
/** Returns true if this value is an array, but not a typed array */
export declare function IsArray(value: unknown): value is ArrayType;
/** Returns true if this value is an undefined */
export declare function IsUndefined(value: unknown): value is undefined;
/** Returns true if this value is an null */
export declare function IsNull(value: unknown): value is null;
/** Returns true if this value is an boolean */
export declare function IsBoolean(value: unknown): value is boolean;
/** Returns true if this value is an number */
export declare function IsNumber(value: unknown): value is number;
/** Returns true if this value is an integer */
export declare function IsInteger(value: unknown): value is number;
/** Returns true if this value is bigint */
export declare function IsBigInt(value: unknown): value is bigint;
/** Returns true if this value is string */
export declare function IsString(value: unknown): value is string;
/** Returns true if this value is a function */
export declare function IsFunction(value: unknown): value is Function;
/** Returns true if this value is a symbol */
export declare function IsSymbol(value: unknown): value is symbol;
/** Returns true if this value is a value type such as number, string, boolean */
export declare function IsValueType(value: unknown): value is ValueType;
