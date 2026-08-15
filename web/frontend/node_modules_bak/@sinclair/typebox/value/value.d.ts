import * as ValueErrors from '../errors/index';
import * as ValueMutate from './mutate';
import * as ValueDelta from './delta';
import * as Types from '../typebox';
/** Functions to perform structural operations on JavaScript values */
export declare namespace Value {
    /** Casts a value into a given type. The return value will retain as much information of the original value as possible. */
    function Cast<T extends Types.TSchema>(schema: T, references: Types.TSchema[], value: unknown): Types.Static<T>;
    /** Casts a value into a given type. The return value will retain as much information of the original value as possible. */
    function Cast<T extends Types.TSchema>(schema: T, value: unknown): Types.Static<T>;
    /** Creates a value from the given type and references */
    function Create<T extends Types.TSchema>(schema: T, references: Types.TSchema[]): Types.Static<T>;
    /** Creates a value from the given type */
    function Create<T extends Types.TSchema>(schema: T): Types.Static<T>;
    /** Returns true if the value matches the given type and references */
    function Check<T extends Types.TSchema>(schema: T, references: Types.TSchema[], value: unknown): value is Types.Static<T>;
    /** Returns true if the value matches the given type */
    function Check<T extends Types.TSchema>(schema: T, value: unknown): value is Types.Static<T>;
    /** Converts any type mismatched values to their target type if a reasonable conversion is possible */
    function Convert<T extends Types.TSchema>(schema: T, references: Types.TSchema[], value: unknown): unknown;
    /** Converts any type mismatched values to their target type if a reasonable conversion is possibl. */
    function Convert<T extends Types.TSchema>(schema: T, value: unknown): unknown;
    /** Returns a structural clone of the given value */
    function Clone<T>(value: T): T;
    /** Decodes a value or throws if error */
    function Decode<T extends Types.TSchema, R = Types.StaticDecode<T>>(schema: T, references: Types.TSchema[], value: unknown): R;
    /** Decodes a value or throws if error */
    function Decode<T extends Types.TSchema, R = Types.StaticDecode<T>>(schema: T, value: unknown): R;
    /** Encodes a value or throws if error */
    function Encode<T extends Types.TSchema, R = Types.StaticEncode<T>>(schema: T, references: Types.TSchema[], value: unknown): R;
    /** Encodes a value or throws if error */
    function Encode<T extends Types.TSchema, R = Types.StaticEncode<T>>(schema: T, value: unknown): R;
    /** Returns an iterator for each error in this value. */
    function Errors<T extends Types.TSchema>(schema: T, references: Types.TSchema[], value: unknown): ValueErrors.ValueErrorIterator;
    /** Returns an iterator for each error in this value. */
    function Errors<T extends Types.TSchema>(schema: T, value: unknown): ValueErrors.ValueErrorIterator;
    /** Returns true if left and right values are structurally equal */
    function Equal<T>(left: T, right: unknown): right is T;
    /** Returns edits to transform the current value into the next value */
    function Diff(current: unknown, next: unknown): ValueDelta.Edit[];
    /** Returns a FNV1A-64 non cryptographic hash of the given value */
    function Hash(value: unknown): bigint;
    /** Returns a new value with edits applied to the given value */
    function Patch<T = any>(current: unknown, edits: ValueDelta.Edit[]): T;
    /** Performs a deep mutable value assignment while retaining internal references. */
    function Mutate(current: ValueMutate.Mutable, next: ValueMutate.Mutable): void;
}
