export declare const Transform: unique symbol;
export declare const Readonly: unique symbol;
export declare const Optional: unique symbol;
export declare const Hint: unique symbol;
export declare const Kind: unique symbol;
export declare const PatternBoolean = "(true|false)";
export declare const PatternNumber = "(0|[1-9][0-9]*)";
export declare const PatternString = "(.*)";
export declare const PatternBooleanExact = "^(true|false)$";
export declare const PatternNumberExact = "^(0|[1-9][0-9]*)$";
export declare const PatternStringExact = "^(.*)$";
export type TupleToIntersect<T extends any[]> = T extends [infer I] ? I : T extends [infer I, ...infer R] ? I & TupleToIntersect<R> : never;
export type TupleToUnion<T extends any[]> = {
    [K in keyof T]: T[K];
}[number];
export type UnionToIntersect<U> = (U extends unknown ? (arg: U) => 0 : never) extends (arg: infer I) => 0 ? I : never;
export type UnionLast<U> = UnionToIntersect<U extends unknown ? (x: U) => 0 : never> extends (x: infer L) => 0 ? L : never;
export type UnionToTuple<U, L = UnionLast<U>> = [U] extends [never] ? [] : [...UnionToTuple<Exclude<U, L>>, L];
export type Discard<T extends unknown[], D extends unknown> = T extends [infer L, ...infer R] ? (L extends D ? Discard<R, D> : [L, ...Discard<R, D>]) : [];
export type Flat<T> = T extends [] ? [] : T extends [infer L] ? [...Flat<L>] : T extends [infer L, ...infer R] ? [...Flat<L>, ...Flat<R>] : [T];
export type Trim<T> = T extends `${' '}${infer U}` ? Trim<U> : T extends `${infer U}${' '}` ? Trim<U> : T;
export type Assert<T, E> = T extends E ? T : never;
export type Evaluate<T> = T extends infer O ? {
    [K in keyof O]: O[K];
} : never;
export type Ensure<T> = T extends infer U ? U : never;
export type AssertProperties<T> = T extends TProperties ? T : TProperties;
export type AssertRest<T, E extends TSchema[] = TSchema[]> = T extends E ? T : [];
export type AssertType<T, E extends TSchema = TSchema> = T extends E ? T : TNever;
export type TReadonlyOptional<T extends TSchema> = TOptional<T> & TReadonly<T>;
export type TReadonly<T extends TSchema> = T & {
    [Readonly]: 'Readonly';
};
export type TOptional<T extends TSchema> = T & {
    [Optional]: 'Optional';
};
export type ReadonlyUnwrapType<T extends TSchema> = T extends TReadonly<infer S> ? ReadonlyUnwrapType<S> : T extends TOptional<infer S> ? TOptional<ReadonlyUnwrapType<S>> : T;
export type ReadonlyUnwrapRest<T extends TSchema[]> = T extends [infer L, ...infer R] ? L extends TReadonly<infer S> ? [ReadonlyUnwrapType<AssertType<S>>, ...ReadonlyUnwrapRest<AssertRest<R>>] : [L, ...ReadonlyUnwrapRest<AssertRest<R>>] : [];
export type OptionalUnwrapType<T extends TSchema> = T extends TReadonly<infer S> ? TReadonly<OptionalUnwrapType<S>> : T extends TOptional<infer S> ? OptionalUnwrapType<S> : T;
export type OptionalUnwrapRest<T extends TSchema[]> = T extends [infer L, ...infer R] ? L extends TOptional<infer S> ? [OptionalUnwrapType<AssertType<S>>, ...OptionalUnwrapRest<AssertRest<R>>] : [L, ...OptionalUnwrapRest<AssertRest<R>>] : [];
export type IntersectOptional<T extends TSchema[]> = T extends [infer L, ...infer R] ? L extends TOptional<AssertType<L>> ? IntersectOptional<AssertRest<R>> : false : true;
export type IntersectResolve<T extends TSchema[], U = OptionalUnwrapRest<AssertRest<T>>> = IntersectOptional<AssertRest<T>> extends true ? TOptional<TIntersect<AssertRest<U>>> : TIntersect<AssertRest<U>>;
export type IntersectType<T extends TSchema[]> = T extends [] ? TNever : T extends [TSchema] ? AssertType<T[0]> : IntersectResolve<T>;
export type UnionOptional<T extends TSchema[]> = T extends [infer L, ...infer R] ? L extends (TOptional<AssertType<L>>) ? true : UnionOptional<AssertRest<R>> : false;
export type UnionResolve<T extends TSchema[], U = OptionalUnwrapRest<AssertRest<T>>> = UnionOptional<AssertRest<T>> extends true ? TOptional<TUnion<AssertRest<U>>> : TUnion<AssertRest<U>>;
export type UnionType<T extends TSchema[]> = T extends [] ? TNever : T extends [TSchema] ? AssertType<T[0]> : UnionResolve<T>;
export interface SchemaOptions {
    $schema?: string;
    /** Id for this schema */
    $id?: string;
    /** Title of this schema */
    title?: string;
    /** Description of this schema */
    description?: string;
    /** Default value for this schema */
    default?: any;
    /** Example values matching this schema */
    examples?: any;
    /** Optional annotation for readOnly */
    readOnly?: boolean;
    /** Optional annotation for writeOnly */
    writeOnly?: boolean;
    [prop: string]: any;
}
export interface TKind {
    [Kind]: string;
}
export interface TSchema extends SchemaOptions, TKind {
    [Readonly]?: string;
    [Optional]?: string;
    [Hint]?: string;
    params: unknown[];
    static: unknown;
}
export type TAnySchema = TSchema | TAny | TArray | TAsyncIterator | TBigInt | TBoolean | TConstructor | TDate | TEnum | TFunction | TInteger | TIntersect | TIterator | TLiteral | TNot | TNull | TNumber | TObject | TPromise | TRecord | TRef | TString | TSymbol | TTemplateLiteral | TThis | TTuple | TUndefined | TUnion | TUint8Array | TUnknown | TVoid;
export interface NumericOptions<N extends number | bigint> extends SchemaOptions {
    exclusiveMaximum?: N;
    exclusiveMinimum?: N;
    maximum?: N;
    minimum?: N;
    multipleOf?: N;
}
export interface TAny extends TSchema {
    [Kind]: 'Any';
    static: any;
}
export interface ArrayOptions extends SchemaOptions {
    /** The minimum number of items in this array */
    minItems?: number;
    /** The maximum number of items in this array */
    maxItems?: number;
    /** Should this schema contain unique items */
    uniqueItems?: boolean;
    /** A schema for which some elements should match */
    contains?: TSchema;
    /** A minimum number of contains schema matches */
    minContains?: number;
    /** A maximum number of contains schema matches */
    maxContains?: number;
}
export interface TArray<T extends TSchema = TSchema> extends TSchema, ArrayOptions {
    [Kind]: 'Array';
    static: Static<T, this['params']>[];
    type: 'array';
    items: T;
}
export interface TAsyncIterator<T extends TSchema = TSchema> extends TSchema {
    [Kind]: 'AsyncIterator';
    static: AsyncIterableIterator<Static<T, this['params']>>;
    type: 'AsyncIterator';
    items: T;
}
export type TAwaitedRest<T extends TSchema[]> = T extends [infer L, ...infer R] ? [TAwaited<AssertType<L>>, ...TAwaitedRest<AssertRest<R>>] : [];
export type TAwaited<T extends TSchema> = T extends TIntersect<infer S> ? TIntersect<TAwaitedRest<S>> : T extends TUnion<infer S> ? TUnion<TAwaitedRest<S>> : T extends TPromise<infer S> ? TAwaited<S> : T;
export interface TBigInt extends TSchema, NumericOptions<bigint> {
    [Kind]: 'BigInt';
    static: bigint;
    type: 'bigint';
}
export interface TBoolean extends TSchema {
    [Kind]: 'Boolean';
    static: boolean;
    type: 'boolean';
}
export type TConstructorParameters<T extends TConstructor<TSchema[], TSchema>> = Ensure<TTuple<T['parameters']>>;
export type TInstanceType<T extends TConstructor<TSchema[], TSchema>> = T['returns'];
export type TCompositeKeys<T extends TObject[]> = T extends [infer L, ...infer R] ? keyof Assert<L, TObject>['properties'] | TCompositeKeys<Assert<R, TObject[]>> : never;
export type TCompositeIndex<T extends TIntersect<TObject[]>, K extends string[]> = K extends [infer L, ...infer R] ? {
    [_ in Assert<L, string>]: TIndexType<T, Assert<L, string>>;
} & TCompositeIndex<T, Assert<R, string[]>> : {};
export type TCompositeReduce<T extends TObject[]> = UnionToTuple<TCompositeKeys<T>> extends infer K ? Evaluate<TCompositeIndex<TIntersect<T>, Assert<K, string[]>>> : {};
export type TComposite<T extends TObject[]> = TIntersect<T> extends TIntersect ? TObject<TCompositeReduce<T>> : TObject<{}>;
export type TConstructorReturnTypeResolve<T extends TSchema, P extends unknown[]> = Static<T, P>;
export type TConstructorParametersResolve<T extends TSchema[], P extends unknown[]> = T extends [infer L extends TSchema, ...infer R extends TSchema[]] ? [Static<L, P>, ...TFunctionParametersResolve<R, P>] : [];
export type TConstructorResolve<T extends TSchema[], U extends TSchema, P extends unknown[]> = Ensure<new (...param: TConstructorParametersResolve<T, P>) => TConstructorReturnTypeResolve<U, P>>;
export interface TConstructor<T extends TSchema[] = TSchema[], U extends TSchema = TSchema> extends TSchema {
    [Kind]: 'Constructor';
    static: TConstructorResolve<T, U, this['params']>;
    type: 'Constructor';
    parameters: T;
    returns: U;
}
export interface DateOptions extends SchemaOptions {
    /** The exclusive maximum timestamp value */
    exclusiveMaximumTimestamp?: number;
    /** The exclusive minimum timestamp value */
    exclusiveMinimumTimestamp?: number;
    /** The maximum timestamp value */
    maximumTimestamp?: number;
    /** The minimum timestamp value */
    minimumTimestamp?: number;
    /** The multiple of timestamp value */
    multipleOfTimestamp?: number;
}
export interface TDate extends TSchema, DateOptions {
    [Kind]: 'Date';
    static: Date;
    type: 'date';
}
export type TEnumRecord = Record<TEnumKey, TEnumValue>;
export type TEnumValue = string | number;
export type TEnumKey = string;
export interface TEnum<T extends Record<string, string | number> = Record<string, string | number>> extends TSchema {
    [Kind]: 'Union';
    [Hint]: 'Enum';
    static: T[keyof T];
    anyOf: TLiteral<T[keyof T]>[];
}
export type TExtends<L extends TSchema, R extends TSchema, T extends TSchema, U extends TSchema> = (Static<L> extends Static<R> ? T : U) extends infer O ? UnionToTuple<O> extends [infer X, infer Y] ? TUnion<[AssertType<X>, AssertType<Y>]> : AssertType<O> : never;
export type TExcludeTemplateLiteralResult<T extends string> = UnionType<AssertRest<UnionToTuple<{
    [K in T]: TLiteral<K>;
}[T]>>>;
export type TExcludeTemplateLiteral<T extends TTemplateLiteral, U extends TSchema> = Exclude<Static<T>, Static<U>> extends infer S ? TExcludeTemplateLiteralResult<Assert<S, string>> : never;
export type TExcludeArray<T extends TSchema[], U extends TSchema> = AssertRest<UnionToTuple<{
    [K in keyof T]: Static<AssertType<T[K]>> extends Static<U> ? never : T[K];
}[number]>> extends infer R ? UnionType<AssertRest<R>> : never;
export type TExclude<T extends TSchema, U extends TSchema> = T extends TTemplateLiteral ? TExcludeTemplateLiteral<T, U> : T extends TUnion<infer S> ? TExcludeArray<S, U> : T extends U ? TNever : T;
export type TExtractTemplateLiteralResult<T extends string> = UnionType<AssertRest<UnionToTuple<{
    [K in T]: TLiteral<K>;
}[T]>>>;
export type TExtractTemplateLiteral<T extends TTemplateLiteral, U extends TSchema> = Extract<Static<T>, Static<U>> extends infer S ? TExtractTemplateLiteralResult<Assert<S, string>> : never;
export type TExtractArray<T extends TSchema[], U extends TSchema> = AssertRest<UnionToTuple<{
    [K in keyof T]: Static<AssertType<T[K]>> extends Static<U> ? T[K] : never;
}[number]>> extends infer R ? UnionType<AssertRest<R>> : never;
export type TExtract<T extends TSchema, U extends TSchema> = T extends TTemplateLiteral ? TExtractTemplateLiteral<T, U> : T extends TUnion<infer S> ? TExtractArray<S, U> : T extends U ? T : T;
export type TFunctionReturnTypeResolve<T extends TSchema, P extends unknown[]> = Static<T, P>;
export type TFunctionParametersResolve<T extends TSchema[], P extends unknown[]> = T extends [infer L extends TSchema, ...infer R extends TSchema[]] ? [Static<L, P>, ...TFunctionParametersResolve<R, P>] : [];
export type TFunctionResolve<T extends TSchema[], U extends TSchema, P extends unknown[]> = Ensure<(...param: TFunctionParametersResolve<T, P>) => TFunctionReturnTypeResolve<U, P>>;
export interface TFunction<T extends TSchema[] = TSchema[], U extends TSchema = TSchema> extends TSchema {
    [Kind]: 'Function';
    static: TFunctionResolve<T, U, this['params']>;
    type: 'Function';
    parameters: T;
    returns: U;
}
export type TIndexRest<T extends TSchema[], K extends TPropertyKey> = T extends [infer L, ...infer R] ? [TIndexType<AssertType<L>, K>, ...TIndexRest<AssertRest<R>, K>] : [];
export type TIndexProperty<T extends TProperties, K extends TPropertyKey> = K extends keyof T ? [T[K]] : [];
export type TIndexTuple<T extends TSchema[], K extends TPropertyKey> = K extends keyof T ? [T[K]] : [];
export type TIndexType<T extends TSchema, K extends TPropertyKey> = T extends TRecursive<infer S> ? TIndexType<S, K> : T extends TIntersect<infer S> ? IntersectType<AssertRest<Discard<Flat<TIndexRest<S, K>>, TNever>>> : T extends TUnion<infer S> ? UnionType<AssertRest<Flat<TIndexRest<S, K>>>> : T extends TObject<infer S> ? UnionType<AssertRest<Flat<TIndexProperty<S, K>>>> : T extends TTuple<infer S> ? UnionType<AssertRest<Flat<TIndexTuple<S, K>>>> : [
];
export type TIndexRestMany<T extends TSchema, K extends TPropertyKey[]> = K extends [infer L, ...infer R] ? [TIndexType<T, Assert<L, TPropertyKey>>, ...TIndexRestMany<T, Assert<R, TPropertyKey[]>>] : [
];
export type TIndex<T extends TSchema, K extends TPropertyKey[]> = T extends TRecursive<infer S> ? TIndex<S, K> : T extends TIntersect ? UnionType<Flat<TIndexRestMany<T, K>>> : T extends TUnion ? UnionType<Flat<TIndexRestMany<T, K>>> : T extends TObject ? UnionType<Flat<TIndexRestMany<T, K>>> : T extends TTuple ? UnionType<Flat<TIndexRestMany<T, K>>> : TNever;
export type TIntrinsicMode = 'Uppercase' | 'Lowercase' | 'Capitalize' | 'Uncapitalize';
export type TIntrinsicTemplateLiteral<T extends TTemplateLiteralKind[], M extends TIntrinsicMode> = M extends ('Lowercase' | 'Uppercase') ? T extends [infer L, ...infer R] ? [TIntrinsic<AssertType<L>, M>, ...TIntrinsicTemplateLiteral<AssertRest<R>, M>] : T : M extends ('Capitalize' | 'Uncapitalize') ? T extends [infer L, ...infer R] ? [TIntrinsic<AssertType<L>, M>, ...R] : T : T;
export type TIntrinsicLiteral<T, M extends TIntrinsicMode> = T extends string ? M extends 'Uncapitalize' ? Uncapitalize<T> : M extends 'Capitalize' ? Capitalize<T> : M extends 'Uppercase' ? Uppercase<T> : M extends 'Lowercase' ? Lowercase<T> : string : T;
export type TIntrinsicRest<T extends TSchema[], M extends TIntrinsicMode> = T extends [infer L, ...infer R] ? [TIntrinsic<AssertType<L>, M>, ...TIntrinsicRest<AssertRest<R>, M>] : [];
export type TIntrinsic<T extends TSchema, M extends TIntrinsicMode> = T extends TTemplateLiteral<infer S> ? TTemplateLiteral<TIntrinsicTemplateLiteral<S, M>> : T extends TUnion<infer S> ? TUnion<TIntrinsicRest<S, M>> : T extends TLiteral<infer S> ? TLiteral<TIntrinsicLiteral<S, M>> : T;
export interface TInteger extends TSchema, NumericOptions<number> {
    [Kind]: 'Integer';
    static: number;
    type: 'integer';
}
export type TUnevaluatedProperties = undefined | TSchema | boolean;
export interface IntersectOptions extends SchemaOptions {
    unevaluatedProperties?: TUnevaluatedProperties;
}
export interface TIntersect<T extends TSchema[] = TSchema[]> extends TSchema, IntersectOptions {
    [Kind]: 'Intersect';
    static: TupleToIntersect<{
        [K in keyof T]: Static<AssertType<T[K]>, this['params']>;
    }>;
    type?: 'object';
    allOf: [...T];
}
export interface TIterator<T extends TSchema = TSchema> extends TSchema {
    [Kind]: 'Iterator';
    static: IterableIterator<Static<T, this['params']>>;
    type: 'Iterator';
    items: T;
}
export type TKeyOfProperties<T extends TSchema> = Discard<Static<T> extends infer S ? UnionToTuple<{
    [K in keyof S]: TLiteral<Assert<K, TLiteralValue>>;
}[keyof S]> : [], undefined>;
export type TKeyOfIndicesArray<T extends TSchema[]> = UnionToTuple<keyof T & `${number}`>;
export type TKeyOfIndices<T extends TSchema[]> = AssertRest<TKeyOfIndicesArray<T> extends infer R ? {
    [K in keyof R]: TLiteral<Assert<R[K], TLiteralValue>>;
} : []>;
export type TKeyOf<T extends TSchema = TSchema> = (T extends TRecursive<infer S> ? TKeyOfProperties<S> : T extends TIntersect ? TKeyOfProperties<T> : T extends TUnion ? TKeyOfProperties<T> : T extends TObject ? TKeyOfProperties<T> : T extends TTuple<infer K> ? TKeyOfIndices<K> : T extends TArray ? [TNumber] : T extends TRecord<infer K> ? [K] : [
]) extends infer R ? UnionType<AssertRest<R>> : never;
export type TLiteralValue = boolean | number | string;
export type TLiteralBoolean = TLiteral<boolean>;
export type TLiteralNumber = TLiteral<number>;
export type TLiteralString = TLiteral<string>;
export interface TLiteral<T extends TLiteralValue = TLiteralValue> extends TSchema {
    [Kind]: 'Literal';
    static: T;
    const: T;
}
export interface TNever extends TSchema {
    [Kind]: 'Never';
    static: never;
    not: {};
}
export interface TNot<T extends TSchema = TSchema> extends TSchema {
    [Kind]: 'Not';
    static: T extends TNot<infer U> ? Static<U> : unknown;
    not: T;
}
export interface TNull extends TSchema {
    [Kind]: 'Null';
    static: null;
    type: 'null';
}
export interface TNumber extends TSchema, NumericOptions<number> {
    [Kind]: 'Number';
    static: number;
    type: 'number';
}
export type ReadonlyOptionalPropertyKeys<T extends TProperties> = {
    [K in keyof T]: T[K] extends TReadonly<TSchema> ? (T[K] extends TOptional<T[K]> ? K : never) : never;
}[keyof T];
export type ReadonlyPropertyKeys<T extends TProperties> = {
    [K in keyof T]: T[K] extends TReadonly<TSchema> ? (T[K] extends TOptional<T[K]> ? never : K) : never;
}[keyof T];
export type OptionalPropertyKeys<T extends TProperties> = {
    [K in keyof T]: T[K] extends TOptional<TSchema> ? (T[K] extends TReadonly<T[K]> ? never : K) : never;
}[keyof T];
export type RequiredPropertyKeys<T extends TProperties> = keyof Omit<T, ReadonlyOptionalPropertyKeys<T> | ReadonlyPropertyKeys<T> | OptionalPropertyKeys<T>>;
export type PropertiesReducer<T extends TProperties, R extends Record<keyof any, unknown>> = Evaluate<(Readonly<Partial<Pick<R, ReadonlyOptionalPropertyKeys<T>>>> & Readonly<Pick<R, ReadonlyPropertyKeys<T>>> & Partial<Pick<R, OptionalPropertyKeys<T>>> & Required<Pick<R, RequiredPropertyKeys<T>>>)>;
export type PropertiesReduce<T extends TProperties, P extends unknown[]> = PropertiesReducer<T, {
    [K in keyof T]: Static<T[K], P>;
}>;
export type TPropertyKey = string | number;
export type TProperties = Record<TPropertyKey, TSchema>;
export type ObjectProperties<T> = T extends TObject<infer U> ? U : never;
export type ObjectPropertyKeys<T> = T extends TObject<infer U> ? keyof U : never;
export type TAdditionalProperties = undefined | TSchema | boolean;
export interface ObjectOptions extends SchemaOptions {
    /** Additional property constraints for this object */
    additionalProperties?: TAdditionalProperties;
    /** The minimum number of properties allowed on this object */
    minProperties?: number;
    /** The maximum number of properties allowed on this object */
    maxProperties?: number;
}
export interface TObject<T extends TProperties = TProperties> extends TSchema, ObjectOptions {
    [Kind]: 'Object';
    static: PropertiesReduce<T, this['params']>;
    additionalProperties?: TAdditionalProperties;
    type: 'object';
    properties: T;
    required?: string[];
}
export type TOmitProperties<T extends TProperties, K extends keyof any> = Evaluate<AssertProperties<Omit<T, K>>>;
export type TOmitRest<T extends TSchema[], K extends keyof any> = AssertRest<{
    [K2 in keyof T]: TOmit<AssertType<T[K2]>, K>;
}>;
export type TOmit<T extends TSchema = TSchema, K extends keyof any = keyof any> = T extends TRecursive<infer S> ? TRecursive<TOmit<S, K>> : T extends TIntersect<infer S> ? TIntersect<TOmitRest<S, K>> : T extends TUnion<infer S> ? TUnion<TOmitRest<S, K>> : T extends TObject<infer S> ? TObject<TOmitProperties<S, K>> : T;
export type TParameters<T extends TFunction> = Ensure<TTuple<T['parameters']>>;
export type TPartialObjectArray<T extends TObject[]> = AssertRest<{
    [K in keyof T]: TPartial<AssertType<T[K], TObject>>;
}, TObject[]>;
export type TPartialRest<T extends TSchema[]> = AssertRest<{
    [K in keyof T]: TPartial<AssertType<T[K]>>;
}>;
export type TPartialProperties<T extends TProperties> = Evaluate<AssertProperties<{
    [K in keyof T]: T[K] extends (TReadonlyOptional<infer S>) ? TReadonlyOptional<S> : T[K] extends (TReadonly<infer S>) ? TReadonlyOptional<S> : T[K] extends (TOptional<infer S>) ? TOptional<S> : TOptional<T[K]>;
}>>;
export type TPartial<T extends TSchema> = T extends TRecursive<infer S> ? TRecursive<TPartial<S>> : T extends TIntersect<infer S> ? TIntersect<TPartialRest<S>> : T extends TUnion<infer S> ? TUnion<TPartialRest<S>> : T extends TObject<infer S> ? TObject<TPartialProperties<S>> : T;
export type TPickProperties<T extends TProperties, K extends keyof any> = Pick<T, Assert<Extract<K, keyof T>, keyof T>> extends infer R ? ({
    [K in keyof R]: AssertType<R[K]> extends TSchema ? R[K] : never;
}) : never;
export type TPickRest<T extends TSchema[], K extends keyof any> = {
    [K2 in keyof T]: TPick<AssertType<T[K2]>, K>;
};
export type TPick<T extends TSchema = TSchema, K extends keyof any = keyof any> = T extends TRecursive<infer S> ? TRecursive<TPick<S, K>> : T extends TIntersect<infer S> ? TIntersect<TPickRest<S, K>> : T extends TUnion<infer S> ? TUnion<TPickRest<S, K>> : T extends TObject<infer S> ? TObject<TPickProperties<S, K>> : T;
export interface TPromise<T extends TSchema = TSchema> extends TSchema {
    [Kind]: 'Promise';
    static: Promise<Static<T, this['params']>>;
    type: 'Promise';
    item: TSchema;
}
export type TRecordFromUnionLiteralString<K extends TLiteralString, T extends TSchema> = {
    [_ in K['const']]: T;
};
export type TRecordFromUnionLiteralNumber<K extends TLiteralNumber, T extends TSchema> = {
    [_ in K['const']]: T;
};
export type TRecordFromEnumKey<K extends Record<string, string | number>, T extends TSchema> = Ensure<TObject<{
    [_ in K[keyof K]]: T;
}>>;
export type TRecordFromUnionRest<K extends TSchema[], T extends TSchema> = K extends [infer L, ...infer R] ? (L extends TUnion<infer S> ? TRecordFromUnionRest<S, T> & TRecordFromUnionRest<AssertRest<R>, T> : L extends TLiteralString ? TRecordFromUnionLiteralString<L, T> & TRecordFromUnionRest<AssertRest<R>, T> : L extends TLiteralNumber ? TRecordFromUnionLiteralNumber<L, T> & TRecordFromUnionRest<AssertRest<R>, T> : {}) : {};
export type TRecordFromUnion<K extends TSchema[], T extends TSchema> = Ensure<TObject<AssertProperties<Evaluate<TRecordFromUnionRest<K, T>>>>>;
export type TRecordFromTemplateLiteralKeyInfinite<K extends TTemplateLiteral, T extends TSchema> = Ensure<TRecord<K, T>>;
export type TRecordFromTemplateLiteralKeyFinite<K extends TTemplateLiteral, T extends TSchema, I = Static<K>> = Ensure<TObject<Evaluate<{
    [_ in Assert<I, string>]: T;
}>>>;
export type TRecordFromTemplateLiteralKey<K extends TTemplateLiteral, T extends TSchema> = IsTemplateLiteralFinite<K> extends false ? TRecordFromTemplateLiteralKeyInfinite<K, T> : TRecordFromTemplateLiteralKeyFinite<K, T>;
export type TRecordFromLiteralStringKey<K extends TLiteralString, T extends TSchema> = Ensure<TObject<{
    [_ in K['const']]: T;
}>>;
export type TRecordFromLiteralNumberKey<K extends TLiteralNumber, T extends TSchema> = Ensure<TObject<{
    [_ in K['const']]: T;
}>>;
export type TRecordFromStringKey<K extends TString, T extends TSchema> = Ensure<TRecord<K, T>>;
export type TRecordFromNumberKey<K extends TNumber, T extends TSchema> = Ensure<TRecord<K, T>>;
export type TRecordFromIntegerKey<K extends TInteger, T extends TSchema> = Ensure<TRecord<K, T>>;
export type TRecordResolve<K extends TSchema, T extends TSchema> = K extends TEnum<infer S> ? TRecordFromEnumKey<S, T> : K extends TUnion<infer S> ? TRecordFromUnion<S, T> : K extends TTemplateLiteral ? TRecordFromTemplateLiteralKey<K, T> : K extends TLiteralString ? TRecordFromLiteralStringKey<K, T> : K extends TLiteralNumber ? TRecordFromLiteralNumberKey<K, T> : K extends TString ? TRecordFromStringKey<K, T> : K extends TNumber ? TRecordFromNumberKey<K, T> : K extends TInteger ? TRecordFromIntegerKey<K, T> : TNever;
export interface TRecord<K extends TSchema = TSchema, T extends TSchema = TSchema> extends TSchema {
    [Kind]: 'Record';
    static: Record<Assert<Static<K>, string | number>, Static<T, this['params']>>;
    type: 'object';
    patternProperties: {
        [pattern: string]: T;
    };
    additionalProperties: TAdditionalProperties;
}
export interface TThis extends TSchema {
    [Kind]: 'This';
    static: this['params'][0];
    $ref: string;
}
export type TRecursiveReduce<T extends TSchema> = Static<T, [TRecursiveReduce<T>]>;
export interface TRecursive<T extends TSchema> extends TSchema {
    [Hint]: 'Recursive';
    static: TRecursiveReduce<T>;
}
export interface TRef<T extends TSchema = TSchema> extends TSchema {
    [Kind]: 'Ref';
    static: Static<T, this['params']>;
    $ref: string;
}
export type TRest<T extends TSchema> = T extends TIntersect<infer R> ? R : T extends TUnion<infer R> ? R : T extends TTuple<infer R> ? R : [];
export type TReturnType<T extends TFunction> = T['returns'];
export type TRequiredRest<T extends TSchema[]> = AssertRest<{
    [K in keyof T]: TRequired<AssertType<T[K]>>;
}>;
export type TRequiredProperties<T extends TProperties> = Evaluate<AssertProperties<{
    [K in keyof T]: T[K] extends (TReadonlyOptional<infer S>) ? TReadonly<S> : T[K] extends (TReadonly<infer S>) ? TReadonly<S> : T[K] extends (TOptional<infer S>) ? S : T[K];
}>>;
export type TRequired<T extends TSchema> = T extends TRecursive<infer S> ? TRecursive<TRequired<S>> : T extends TIntersect<infer S> ? TIntersect<TRequiredRest<S>> : T extends TUnion<infer S> ? TUnion<TRequiredRest<S>> : T extends TObject<infer S> ? TObject<TRequiredProperties<S>> : T;
export type StringFormatOption = 'date-time' | 'time' | 'date' | 'email' | 'idn-email' | 'hostname' | 'idn-hostname' | 'ipv4' | 'ipv6' | 'uri' | 'uri-reference' | 'iri' | 'uuid' | 'iri-reference' | 'uri-template' | 'json-pointer' | 'relative-json-pointer' | 'regex' | ({} & string);
export type StringContentEncodingOption = '7bit' | '8bit' | 'binary' | 'quoted-printable' | 'base64' | ({} & string);
export interface StringOptions extends SchemaOptions {
    /** The maximum string length */
    maxLength?: number;
    /** The minimum string length */
    minLength?: number;
    /** A regular expression pattern this string should match */
    pattern?: string;
    /** A format this string should match */
    format?: StringFormatOption;
    /** The content encoding for this string */
    contentEncoding?: StringContentEncodingOption;
    /** The content media type for this string */
    contentMediaType?: string;
}
export interface TString extends TSchema, StringOptions {
    [Kind]: 'String';
    static: string;
    type: 'string';
}
export type SymbolValue = string | number | undefined;
export interface TSymbol extends TSchema, SchemaOptions {
    [Kind]: 'Symbol';
    static: symbol;
    type: 'symbol';
}
export type TTemplateLiteralDslParserUnionLiteral<T extends string> = T extends `${infer L}|${infer R}` ? [TLiteral<Trim<L>>, ...TTemplateLiteralDslParserUnionLiteral<R>] : T extends `${infer L}` ? [TLiteral<Trim<L>>] : [
];
export type TTemplateLiteralDslParserUnion<T extends string> = UnionType<TTemplateLiteralDslParserUnionLiteral<T>>;
export type TTemplateLiteralDslParserTerminal<T extends string> = T extends 'boolean' ? TBoolean : T extends 'bigint' ? TBigInt : T extends 'number' ? TNumber : T extends 'string' ? TString : TTemplateLiteralDslParserUnion<T>;
export type TTemplateLiteralDslParserTemplate<T extends string> = T extends `{${infer L}}${infer R}` ? [TTemplateLiteralDslParserTerminal<L>, ...TTemplateLiteralDslParserTemplate<R>] : T extends `${infer L}$${infer R}` ? [TLiteral<L>, ...TTemplateLiteralDslParserTemplate<R>] : T extends `${infer L}` ? [TLiteral<L>] : [
];
export type TTemplateLiteralDslParser<T extends string> = Ensure<TTemplateLiteral<Assert<TTemplateLiteralDslParserTemplate<T>, TTemplateLiteralKind[]>>>;
export type IsTemplateLiteralFiniteCheck<T> = T extends TTemplateLiteral<infer U> ? IsTemplateLiteralFiniteArray<Assert<U, TTemplateLiteralKind[]>> : T extends TUnion<infer U> ? IsTemplateLiteralFiniteArray<Assert<U, TTemplateLiteralKind[]>> : T extends TString ? false : T extends TBoolean ? false : T extends TNumber ? false : T extends TInteger ? false : T extends TBigInt ? false : T extends TLiteral ? true : false;
export type IsTemplateLiteralFiniteArray<T extends TTemplateLiteralKind[]> = T extends [infer L, ...infer R] ? IsTemplateLiteralFiniteCheck<L> extends false ? false : IsTemplateLiteralFiniteArray<Assert<R, TTemplateLiteralKind[]>> : true;
export type IsTemplateLiteralFinite<T> = T extends TTemplateLiteral<infer U> ? IsTemplateLiteralFiniteArray<U> : false;
export type TTemplateLiteralKind = TUnion | TLiteral | TInteger | TTemplateLiteral | TNumber | TBigInt | TString | TBoolean | TNever;
export type TTemplateLiteralConst<T, Acc extends string> = T extends TUnion<infer U> ? {
    [K in keyof U]: TTemplateLiteralUnion<Assert<[U[K]], TTemplateLiteralKind[]>, Acc>;
}[number] : T extends TTemplateLiteral ? `${Static<T>}` : T extends TLiteral<infer U> ? `${U}` : T extends TString ? `${string}` : T extends TNumber ? `${number}` : T extends TBigInt ? `${bigint}` : T extends TBoolean ? `${boolean}` : never;
export type TTemplateLiteralUnion<T extends TTemplateLiteralKind[], Acc extends string> = T extends [infer L, ...infer R] ? `${TTemplateLiteralConst<L, Acc>}${TTemplateLiteralUnion<Assert<R, TTemplateLiteralKind[]>, Acc>}` : Acc;
export type TTemplateLiteralKeyRest<T extends TTemplateLiteral> = Assert<UnionToTuple<Static<T>>, TPropertyKey[]>;
export interface TTemplateLiteral<T extends TTemplateLiteralKind[] = TTemplateLiteralKind[]> extends TSchema {
    [Kind]: 'TemplateLiteral';
    static: TTemplateLiteralUnion<T, ''>;
    type: 'string';
    pattern: string;
}
export type DecodeProperties<T extends TProperties> = {
    [K in keyof T]: DecodeType<T[K]>;
};
export type DecodeRest<T extends TSchema[]> = T extends [infer L extends TSchema, ...infer R extends TSchema[]] ? [DecodeType<L>, ...DecodeRest<R>] : [];
export type DecodeType<T extends TSchema> = (T extends TOptional<infer S extends TSchema> ? TOptional<DecodeType<S>> : T extends TReadonly<infer S extends TSchema> ? TReadonly<DecodeType<S>> : T extends TTransform<infer _, infer R> ? TUnsafe<R> : T extends TArray<infer S extends TSchema> ? TArray<DecodeType<S>> : T extends TAsyncIterator<infer S extends TSchema> ? TAsyncIterator<DecodeType<S>> : T extends TConstructor<infer P extends TSchema[], infer R extends TSchema> ? TConstructor<DecodeRest<P>, DecodeType<R>> : T extends TEnum<infer S> ? TEnum<S> : T extends TFunction<infer P extends TSchema[], infer R extends TSchema> ? TFunction<DecodeRest<P>, DecodeType<R>> : T extends TIntersect<infer S extends TSchema[]> ? TIntersect<DecodeRest<S>> : T extends TIterator<infer S extends TSchema> ? TIterator<DecodeType<S>> : T extends TNot<infer S extends TSchema> ? TNot<DecodeType<S>> : T extends TObject<infer S> ? TObject<Evaluate<DecodeProperties<S>>> : T extends TPromise<infer S extends TSchema> ? TPromise<DecodeType<S>> : T extends TRecord<infer K, infer S> ? TRecord<K, DecodeType<S>> : T extends TRecursive<infer S extends TSchema> ? TRecursive<DecodeType<S>> : T extends TRef<infer S extends TSchema> ? TRef<DecodeType<S>> : T extends TTuple<infer S extends TSchema[]> ? TTuple<DecodeRest<S>> : T extends TUnion<infer S extends TSchema[]> ? TUnion<DecodeRest<S>> : T);
export type TransformFunction<T = any, U = any> = (value: T) => U;
export interface TransformOptions<I extends TSchema = TSchema, O extends unknown = unknown> {
    Decode: TransformFunction<StaticDecode<I>, O>;
    Encode: TransformFunction<O, StaticDecode<I>>;
}
export type TTransformResolve<T extends TSchema, P extends unknown[] = []> = T extends TTransform<infer _, infer S> ? S : Static<T, P>;
export interface TTransform<I extends TSchema = TSchema, O extends unknown = unknown> extends TSchema {
    static: TTransformResolve<I, this['params']>;
    [Transform]: TransformOptions<I, O>;
    [key: string]: any;
}
export type TTupleRest<T extends TSchema[], P extends unknown[]> = T extends [infer L, ...infer R] ? [Static<AssertType<L>, P>, ...TTupleRest<AssertRest<R>, P>] : [];
export interface TTuple<T extends TSchema[] = TSchema[]> extends TSchema {
    [Kind]: 'Tuple';
    static: TTupleRest<T, this['params']>;
    type: 'array';
    items?: T;
    additionalItems?: false;
    minItems: number;
    maxItems: number;
}
export interface TUndefined extends TSchema {
    [Kind]: 'Undefined';
    static: undefined;
    type: 'undefined';
}
export type TLiteralUnionReduce<T extends TLiteral<string | number>[]> = T extends [infer L, ...infer R] ? [Assert<L, TLiteral<string | number>>['const'], ...TLiteralUnionReduce<Assert<R, TLiteral<string | number>[]>>] : [
];
export type TUnionLiteralKeyRest<T extends TUnion<TLiteral<string | number>[]>> = T extends TUnion<infer S> ? TLiteralUnionReduce<Assert<S, TLiteral<string | number>[]>> : [
];
export type TUnionTemplateLiteral<T extends TTemplateLiteral, S extends string = Static<T>> = Ensure<UnionType<Assert<UnionToTuple<{
    [K in S]: TLiteral<K>;
}[S]>, TLiteral[]>>>;
export interface TUnion<T extends TSchema[] = TSchema[]> extends TSchema {
    [Kind]: 'Union';
    static: {
        [K in keyof T]: T[K] extends TSchema ? Static<T[K], this['params']> : never;
    }[number];
    anyOf: T;
}
export interface Uint8ArrayOptions extends SchemaOptions {
    maxByteLength?: number;
    minByteLength?: number;
}
export interface TUint8Array extends TSchema, Uint8ArrayOptions {
    [Kind]: 'Uint8Array';
    static: Uint8Array;
    type: 'uint8array';
}
export interface TUnknown extends TSchema {
    [Kind]: 'Unknown';
    static: unknown;
}
export interface UnsafeOptions extends SchemaOptions {
    [Kind]?: string;
}
export interface TUnsafe<T> extends TSchema {
    [Kind]: string;
    static: T;
}
export interface TVoid extends TSchema {
    [Kind]: 'Void';
    static: void;
    type: 'void';
}
/** Creates an decoded static type from a TypeBox type */
export type StaticDecode<T extends TSchema, P extends unknown[] = []> = Static<DecodeType<T>, P>;
/** Creates an encoded static type from a TypeBox type */
export type StaticEncode<T extends TSchema, P extends unknown[] = []> = Static<T, P>;
/** Creates a static type from a TypeBox type */
export type Static<T extends TSchema, P extends unknown[] = []> = (T & {
    params: P;
})['static'];
export type TypeRegistryValidationFunction<TSchema> = (schema: TSchema, value: unknown) => boolean;
/** A registry for user defined types */
export declare namespace TypeRegistry {
    /** Returns the entries in this registry */
    function Entries(): Map<string, TypeRegistryValidationFunction<any>>;
    /** Clears all user defined types */
    function Clear(): void;
    /** Deletes a registered type */
    function Delete(kind: string): boolean;
    /** Returns true if this registry contains this kind */
    function Has(kind: string): boolean;
    /** Sets a validation function for a user defined type */
    function Set<TSchema = unknown>(kind: string, func: TypeRegistryValidationFunction<TSchema>): void;
    /** Gets a custom validation function for a user defined type */
    function Get(kind: string): TypeRegistryValidationFunction<any> | undefined;
}
export declare class TypeBoxError extends Error {
    constructor(message: string);
}
export type FormatRegistryValidationFunction = (value: string) => boolean;
/** A registry for user defined string formats */
export declare namespace FormatRegistry {
    /** Returns the entries in this registry */
    function Entries(): Map<string, FormatRegistryValidationFunction>;
    /** Clears all user defined string formats */
    function Clear(): void;
    /** Deletes a registered format */
    function Delete(format: string): boolean;
    /** Returns true if the user defined string format exists */
    function Has(format: string): boolean;
    /** Sets a validation function for a user defined string format */
    function Set(format: string, func: FormatRegistryValidationFunction): void;
    /** Gets a validation function for a user defined string format */
    function Get(format: string): FormatRegistryValidationFunction | undefined;
}
/** Provides functions to type guard raw JavaScript values */
export declare namespace ValueGuard {
    /** Returns true if this value is an array */
    function IsArray(value: unknown): value is unknown[];
    /** Returns true if this value is bigint */
    function IsBigInt(value: unknown): value is bigint;
    /** Returns true if this value is a boolean */
    function IsBoolean(value: unknown): value is boolean;
    /** Returns true if this value is a Date object */
    function IsDate(value: unknown): value is Date;
    /** Returns true if this value is null */
    function IsNull(value: unknown): value is null;
    /** Returns true if this value is number */
    function IsNumber(value: unknown): value is number;
    /** Returns true if this value is an object */
    function IsObject(value: unknown): value is Record<PropertyKey, unknown>;
    /** Returns true if this value is string */
    function IsString(value: unknown): value is string;
    /** Returns true if this value is a Uint8Array */
    function IsUint8Array(value: unknown): value is Uint8Array;
    /** Returns true if this value is undefined */
    function IsUndefined(value: unknown): value is undefined;
}
export declare class TypeGuardUnknownTypeError extends TypeBoxError {
}
/** Provides functions to test if JavaScript values are TypeBox types */
export declare namespace TypeGuard {
    /** Returns true if the given value is TAny */
    function TAny(schema: unknown): schema is TAny;
    /** Returns true if the given value is TArray */
    function TArray(schema: unknown): schema is TArray;
    /** Returns true if the given value is TAsyncIterator */
    function TAsyncIterator(schema: unknown): schema is TAsyncIterator;
    /** Returns true if the given value is TBigInt */
    function TBigInt(schema: unknown): schema is TBigInt;
    /** Returns true if the given value is TBoolean */
    function TBoolean(schema: unknown): schema is TBoolean;
    /** Returns true if the given value is TConstructor */
    function TConstructor(schema: unknown): schema is TConstructor;
    /** Returns true if the given value is TDate */
    function TDate(schema: unknown): schema is TDate;
    /** Returns true if the given value is TFunction */
    function TFunction(schema: unknown): schema is TFunction;
    /** Returns true if the given value is TInteger */
    function TInteger(schema: unknown): schema is TInteger;
    /** Returns true if the given value is TIntersect */
    function TIntersect(schema: unknown): schema is TIntersect;
    /** Returns true if the given value is TIterator */
    function TIterator(schema: unknown): schema is TIterator;
    /** Returns true if the given value is a TKind with the given name. */
    function TKindOf<T extends string>(schema: unknown, kind: T): schema is Record<PropertyKey, unknown> & {
        [Kind]: T;
    };
    /** Returns true if the given value is TKind */
    function TKind(schema: unknown): schema is Record<PropertyKey, unknown> & {
        [Kind]: string;
    };
    /** Returns true if the given value is TLiteral<string> */
    function TLiteralString(schema: unknown): schema is TLiteral<string>;
    /** Returns true if the given value is TLiteral<number> */
    function TLiteralNumber(schema: unknown): schema is TLiteral<number>;
    /** Returns true if the given value is TLiteral<boolean> */
    function TLiteralBoolean(schema: unknown): schema is TLiteral<boolean>;
    /** Returns true if the given value is TLiteral */
    function TLiteral(schema: unknown): schema is TLiteral;
    /** Returns true if the given value is TNever */
    function TNever(schema: unknown): schema is TNever;
    /** Returns true if the given value is TNot */
    function TNot(schema: unknown): schema is TNot;
    /** Returns true if the given value is TNull */
    function TNull(schema: unknown): schema is TNull;
    /** Returns true if the given value is TNumber */
    function TNumber(schema: unknown): schema is TNumber;
    /** Returns true if the given value is TObject */
    function TObject(schema: unknown): schema is TObject;
    /** Returns true if the given value is TPromise */
    function TPromise(schema: unknown): schema is TPromise;
    /** Returns true if the given value is TRecord */
    function TRecord(schema: unknown): schema is TRecord;
    /** Returns true if this value is TRecursive */
    function TRecursive(schema: unknown): schema is {
        [Hint]: 'Recursive';
    };
    /** Returns true if the given value is TRef */
    function TRef(schema: unknown): schema is TRef;
    /** Returns true if the given value is TString */
    function TString(schema: unknown): schema is TString;
    /** Returns true if the given value is TSymbol */
    function TSymbol(schema: unknown): schema is TSymbol;
    /** Returns true if the given value is TTemplateLiteral */
    function TTemplateLiteral(schema: unknown): schema is TTemplateLiteral;
    /** Returns true if the given value is TThis */
    function TThis(schema: unknown): schema is TThis;
    /** Returns true of this value is TTransform */
    function TTransform(schema: unknown): schema is {
        [Transform]: TransformOptions;
    };
    /** Returns true if the given value is TTuple */
    function TTuple(schema: unknown): schema is TTuple;
    /** Returns true if the given value is TUndefined */
    function TUndefined(schema: unknown): schema is TUndefined;
    /** Returns true if the given value is TUnion<Literal<string | number>[]> */
    function TUnionLiteral(schema: unknown): schema is TUnion<TLiteral[]>;
    /** Returns true if the given value is TUnion */
    function TUnion(schema: unknown): schema is TUnion;
    /** Returns true if the given value is TUint8Array */
    function TUint8Array(schema: unknown): schema is TUint8Array;
    /** Returns true if the given value is TUnknown */
    function TUnknown(schema: unknown): schema is TUnknown;
    /** Returns true if the given value is a raw TUnsafe */
    function TUnsafe(schema: unknown): schema is TUnsafe<unknown>;
    /** Returns true if the given value is TVoid */
    function TVoid(schema: unknown): schema is TVoid;
    /** Returns true if this value has a Readonly symbol */
    function TReadonly<T extends TSchema>(schema: T): schema is TReadonly<T>;
    /** Returns true if this value has a Optional symbol */
    function TOptional<T extends TSchema>(schema: T): schema is TOptional<T>;
    /** Returns true if the given value is TSchema */
    function TSchema(schema: unknown): schema is TSchema;
}
/** Fast undefined check used for properties of type undefined */
export declare namespace ExtendsUndefined {
    function Check(schema: TSchema): boolean;
}
export declare class TypeExtendsError extends TypeBoxError {
}
export declare enum TypeExtendsResult {
    Union = 0,
    True = 1,
    False = 2
}
export declare namespace TypeExtends {
    function Extends(left: TSchema, right: TSchema): TypeExtendsResult;
}
/** Specialized Clone for Types */
export declare namespace TypeClone {
    /** Clones a Rest */
    function Rest<T extends TSchema[]>(schemas: [...T]): T;
    /** Clones a Type */
    function Type<T extends TSchema>(schema: T, options?: SchemaOptions): T;
}
export declare namespace IndexedAccessor {
    function Resolve(schema: TSchema, keys: TPropertyKey[], options?: SchemaOptions): TSchema;
}
export declare namespace Intrinsic {
    /** Applies an intrinsic string manipulation to the given type. */
    function Map<T extends TSchema, M extends TIntrinsicMode>(schema: T, mode: M): TIntrinsic<T, M>;
}
export declare namespace ObjectMap {
    function Map<T = TSchema>(schema: TSchema, callback: (object: TObject) => TObject, options: SchemaOptions): T;
}
export interface KeyResolverOptions {
    includePatterns: boolean;
}
export declare namespace KeyResolver {
    /** Resolves an array of keys in this schema */
    function ResolveKeys(schema: TSchema, options: KeyResolverOptions): string[];
    /** Resolves a regular expression pattern matching all keys in this schema */
    function ResolvePattern(schema: TSchema): string;
}
export declare class KeyArrayResolverError extends TypeBoxError {
}
export declare namespace KeyArrayResolver {
    /** Resolves an array of string[] keys from the given schema or array type. */
    function Resolve(schema: TSchema | string[]): string[];
}
export declare namespace UnionResolver {
    /** Returns a resolved union with interior unions flattened */
    function Resolve(union: TUnion): TUnion;
}
export declare class TemplateLiteralPatternError extends TypeBoxError {
}
export declare namespace TemplateLiteralPattern {
    function Create(kinds: TTemplateLiteralKind[]): string;
}
export declare namespace TemplateLiteralResolver {
    /** Resolves a template literal as a TUnion */
    function Resolve(template: TTemplateLiteral): TString | TUnion | TLiteral;
}
export declare class TemplateLiteralParserError extends TypeBoxError {
}
export declare namespace TemplateLiteralParser {
    type Expression = And | Or | Const;
    type Const = {
        type: 'const';
        const: string;
    };
    type And = {
        type: 'and';
        expr: Expression[];
    };
    type Or = {
        type: 'or';
        expr: Expression[];
    };
    /** Parses a pattern and returns an expression tree */
    function Parse(pattern: string): Expression;
    /** Parses a pattern and strips forward and trailing ^ and $ */
    function ParseExact(pattern: string): Expression;
}
export declare class TemplateLiteralFiniteError extends TypeBoxError {
}
export declare namespace TemplateLiteralFinite {
    function Check(expression: TemplateLiteralParser.Expression): boolean;
}
export declare class TemplateLiteralGeneratorError extends TypeBoxError {
}
export declare namespace TemplateLiteralGenerator {
    function Generate(expression: TemplateLiteralParser.Expression): IterableIterator<string>;
}
export declare namespace TemplateLiteralDslParser {
    function Parse(template_dsl: string): TTemplateLiteralKind[];
}
export declare class TransformDecodeBuilder<T extends TSchema> {
    private readonly schema;
    constructor(schema: T);
    Decode<U extends unknown, D extends TransformFunction<StaticDecode<T>, U>>(decode: D): TransformEncodeBuilder<T, D>;
}
export declare class TransformEncodeBuilder<T extends TSchema, D extends TransformFunction> {
    private readonly schema;
    private readonly decode;
    constructor(schema: T, decode: D);
    Encode<E extends TransformFunction<ReturnType<D>, StaticDecode<T>>>(encode: E): TTransform<T, ReturnType<D>>;
}
export declare class TypeBuilderError extends TypeBoxError {
}
export declare class TypeBuilder {
    /** `[Internal]` Creates a schema without `static` and `params` types */
    protected Create<T>(schema: Omit<T, 'static' | 'params'>): T;
    /** `[Internal]` Throws a TypeBuilder error with the given message */
    protected Throw(message: string): never;
    /** `[Internal]` Discards property keys from the given record type */
    protected Discard(record: Record<PropertyKey, any>, keys: PropertyKey[]): any;
    /** `[Json]` Omits compositing symbols from this schema */
    Strict<T extends TSchema>(schema: T): T;
}
export declare class JsonTypeBuilder extends TypeBuilder {
    /** `[Json]` Creates a Readonly and Optional property */
    ReadonlyOptional<T extends TSchema>(schema: T): TReadonly<TOptional<T>>;
    /** `[Json]` Creates a Readonly property */
    Readonly<T extends TSchema>(schema: T): TReadonly<T>;
    /** `[Json]` Creates an Optional property */
    Optional<T extends TSchema>(schema: T): TOptional<T>;
    /** `[Json]` Creates an Any type */
    Any(options?: SchemaOptions): TAny;
    /** `[Json]` Creates an Array type */
    Array<T extends TSchema>(schema: T, options?: ArrayOptions): TArray<T>;
    /** `[Json]` Creates a Boolean type */
    Boolean(options?: SchemaOptions): TBoolean;
    /** `[Json]` Intrinsic function to Capitalize LiteralString types */
    Capitalize<T extends TSchema>(schema: T, options?: SchemaOptions): TIntrinsic<T, 'Capitalize'>;
    /** `[Json]` Creates a Composite object type */
    Composite<T extends TObject[]>(objects: [...T], options?: ObjectOptions): TComposite<T>;
    /** `[Json]` Creates a Enum type */
    Enum<V extends TEnumValue, T extends Record<TEnumKey, V>>(item: T, options?: SchemaOptions): TEnum<T>;
    /** `[Json]` Creates a Conditional type */
    Extends<L extends TSchema, R extends TSchema, T extends TSchema, U extends TSchema>(left: L, right: R, trueType: T, falseType: U, options?: SchemaOptions): TExtends<L, R, T, U>;
    /** `[Json]` Constructs a type by excluding from unionType all union members that are assignable to excludedMembers */
    Exclude<L extends TSchema, R extends TSchema>(unionType: L, excludedMembers: R, options?: SchemaOptions): TExclude<L, R>;
    /** `[Json]` Constructs a type by extracting from type all union members that are assignable to union */
    Extract<L extends TSchema, R extends TSchema>(type: L, union: R, options?: SchemaOptions): TExtract<L, R>;
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index<T extends TArray, K extends TNumber>(schema: T, keys: K, options?: SchemaOptions): AssertType<T['items']>;
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index<T extends TTuple, K extends (keyof Static<T>)[]>(schema: T, keys: [...K], options?: SchemaOptions): TIndex<T, Assert<K, TPropertyKey[]>>;
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index<T extends TTuple, K extends TNumber>(schema: T, keys: K, options?: SchemaOptions): UnionType<AssertRest<T['items']>>;
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index<T extends TSchema, K extends TTemplateLiteral>(schema: T, keys: K, options?: SchemaOptions): TIndex<T, TTemplateLiteralKeyRest<K>>;
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index<T extends TSchema, K extends TLiteral<TPropertyKey>>(schema: T, keys: K, options?: SchemaOptions): TIndex<T, [K['const']]>;
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index<T extends TSchema, K extends (keyof Static<T>)[]>(schema: T, keys: [...K], options?: SchemaOptions): TIndex<T, Assert<K, TPropertyKey[]>>;
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index<T extends TSchema, K extends TUnion<TLiteral<TPropertyKey>[]>>(schema: T, keys: K, options?: SchemaOptions): TIndex<T, TUnionLiteralKeyRest<K>>;
    /** `[Json]` Returns an Indexed property type for the given keys */
    Index<T extends TSchema, K extends TSchema>(schema: T, key: K, options?: SchemaOptions): TSchema;
    /** `[Json]` Creates an Integer type */
    Integer(options?: NumericOptions<number>): TInteger;
    /** `[Json]` Creates an Intersect type */
    Intersect(allOf: [], options?: SchemaOptions): TNever;
    /** `[Json]` Creates an Intersect type */
    Intersect<T extends [TSchema]>(allOf: [...T], options?: SchemaOptions): T[0];
    /** `[Json]` Creates an Intersect type */
    Intersect<T extends TSchema[]>(allOf: [...T], options?: IntersectOptions): TIntersect<T>;
    /** `[Json]` Creates a KeyOf type */
    KeyOf<T extends TSchema>(schema: T, options?: SchemaOptions): TKeyOf<T>;
    /** `[Json]` Creates a Literal type */
    Literal<T extends TLiteralValue>(value: T, options?: SchemaOptions): TLiteral<T>;
    /** `[Json]` Intrinsic function to Lowercase LiteralString types */
    Lowercase<T extends TSchema>(schema: T, options?: SchemaOptions): TIntrinsic<T, 'Lowercase'>;
    /** `[Json]` Creates a Never type */
    Never(options?: SchemaOptions): TNever;
    /** `[Json]` Creates a Not type */
    Not<T extends TSchema>(schema: T, options?: SchemaOptions): TNot<T>;
    /** `[Json]` Creates a Null type */
    Null(options?: SchemaOptions): TNull;
    /** `[Json]` Creates a Number type */
    Number(options?: NumericOptions<number>): TNumber;
    /** `[Json]` Creates an Object type */
    Object<T extends TProperties>(properties: T, options?: ObjectOptions): TObject<T>;
    /** `[Json]` Constructs a type whose keys are omitted from the given type */
    Omit<T extends TSchema, K extends (keyof Static<T>)[]>(schema: T, keys: readonly [...K], options?: SchemaOptions): TOmit<T, K[number]>;
    /** `[Json]` Constructs a type whose keys are omitted from the given type */
    Omit<T extends TSchema, K extends TUnion<TLiteral<string>[]>>(schema: T, keys: K, options?: SchemaOptions): TOmit<T, TUnionLiteralKeyRest<K>[number]>;
    /** `[Json]` Constructs a type whose keys are omitted from the given type */
    Omit<T extends TSchema, K extends TLiteral<string>>(schema: T, key: K, options?: SchemaOptions): TOmit<T, K['const']>;
    /** `[Json]` Constructs a type whose keys are omitted from the given type */
    Omit<T extends TSchema, K extends TTemplateLiteral>(schema: T, key: K, options?: SchemaOptions): TOmit<T, TTemplateLiteralKeyRest<K>[number]>;
    /** `[Json]` Constructs a type whose keys are omitted from the given type */
    Omit<T extends TSchema, K extends TNever>(schema: T, key: K, options?: SchemaOptions): TOmit<T, never>;
    /** `[Json]` Constructs a type where all properties are optional */
    Partial<T extends TSchema>(schema: T, options?: ObjectOptions): TPartial<T>;
    /** `[Json]` Constructs a type whose keys are picked from the given type */
    Pick<T extends TSchema, K extends (keyof Static<T>)[]>(schema: T, keys: readonly [...K], options?: SchemaOptions): TPick<T, K[number]>;
    /** `[Json]` Constructs a type whose keys are picked from the given type */
    Pick<T extends TSchema, K extends TUnion<TLiteral<string>[]>>(schema: T, keys: K, options?: SchemaOptions): TPick<T, TUnionLiteralKeyRest<K>[number]>;
    /** `[Json]` Constructs a type whose keys are picked from the given type */
    Pick<T extends TSchema, K extends TLiteral<string>>(schema: T, key: K, options?: SchemaOptions): TPick<T, K['const']>;
    /** `[Json]` Constructs a type whose keys are picked from the given type */
    Pick<T extends TSchema, K extends TTemplateLiteral>(schema: T, key: K, options?: SchemaOptions): TPick<T, TTemplateLiteralKeyRest<K>[number]>;
    /** `[Json]` Constructs a type whose keys are picked from the given type */
    Pick<T extends TSchema, K extends TNever>(schema: T, key: K, options?: SchemaOptions): TPick<T, never>;
    /** `[Json]` Creates a Record type */
    Record<K extends TSchema, T extends TSchema>(key: K, schema: T, options?: ObjectOptions): TRecordResolve<K, T>;
    /** `[Json]` Creates a Recursive type */
    Recursive<T extends TSchema>(callback: (thisType: TThis) => T, options?: SchemaOptions): TRecursive<T>;
    /** `[Json]` Creates a Ref type. The referenced type must contain a $id */
    Ref<T extends TSchema>(schema: T, options?: SchemaOptions): TRef<T>;
    /** `[Json]` Creates a Ref type. */
    Ref<T extends TSchema>($ref: string, options?: SchemaOptions): TRef<T>;
    /** `[Json]` Constructs a type where all properties are required */
    Required<T extends TSchema>(schema: T, options?: SchemaOptions): TRequired<T>;
    /** `[Json]` Extracts interior Rest elements from Tuple, Intersect and Union types */
    Rest<T extends TSchema>(schema: T): TRest<T>;
    /** `[Json]` Creates a String type */
    String(options?: StringOptions): TString;
    /** `[Json]` Creates a TemplateLiteral type from template dsl string */
    TemplateLiteral<T extends string>(templateDsl: T, options?: SchemaOptions): TTemplateLiteralDslParser<T>;
    /** `[Json]` Creates a TemplateLiteral type */
    TemplateLiteral<T extends TTemplateLiteralKind[]>(kinds: [...T], options?: SchemaOptions): TTemplateLiteral<T>;
    /** `[Json]` Creates a Transform type */
    Transform<I extends TSchema>(schema: I): TransformDecodeBuilder<I>;
    /** `[Json]` Creates a Tuple type */
    Tuple<T extends TSchema[]>(items: [...T], options?: SchemaOptions): TTuple<T>;
    /** `[Json]` Intrinsic function to Uncapitalize LiteralString types */
    Uncapitalize<T extends TSchema>(schema: T, options?: SchemaOptions): TIntrinsic<T, 'Uncapitalize'>;
    /** `[Json]` Creates a Union type */
    Union(anyOf: [], options?: SchemaOptions): TNever;
    /** `[Json]` Creates a Union type */
    Union<T extends [TSchema]>(anyOf: [...T], options?: SchemaOptions): T[0];
    /** `[Json]` Creates a Union type */
    Union<T extends TSchema[]>(anyOf: [...T], options?: SchemaOptions): TUnion<T>;
    /** `[Json-Experimental]` Converts a TemplateLiteral into a Union */
    Union<T extends TTemplateLiteral>(template: T): TUnionTemplateLiteral<T>;
    /** `[Json]` Creates an Unknown type */
    Unknown(options?: SchemaOptions): TUnknown;
    /** `[Json]` Creates a Unsafe type that will infers as the generic argument T */
    Unsafe<T>(options?: UnsafeOptions): TUnsafe<T>;
    /** `[Json]` Intrinsic function to Uppercase LiteralString types */
    Uppercase<T extends TSchema>(schema: T, options?: SchemaOptions): TIntrinsic<T, 'Uppercase'>;
}
export declare class JavaScriptTypeBuilder extends JsonTypeBuilder {
    /** `[JavaScript]` Creates a AsyncIterator type */
    AsyncIterator<T extends TSchema>(items: T, options?: SchemaOptions): TAsyncIterator<T>;
    /** `[JavaScript]` Constructs a type by recursively unwrapping Promise types */
    Awaited<T extends TSchema>(schema: T, options?: SchemaOptions): TAwaited<T>;
    /** `[JavaScript]` Creates a BigInt type */
    BigInt(options?: NumericOptions<bigint>): TBigInt;
    /** `[JavaScript]` Extracts the ConstructorParameters from the given Constructor type */
    ConstructorParameters<T extends TConstructor<TSchema[], TSchema>>(schema: T, options?: SchemaOptions): TConstructorParameters<T>;
    /** `[JavaScript]` Creates a Constructor type */
    Constructor<T extends TSchema[], U extends TSchema>(parameters: [...T], returns: U, options?: SchemaOptions): TConstructor<T, U>;
    /** `[JavaScript]` Creates a Date type */
    Date(options?: DateOptions): TDate;
    /** `[JavaScript]` Creates a Function type */
    Function<T extends TSchema[], U extends TSchema>(parameters: [...T], returns: U, options?: SchemaOptions): TFunction<T, U>;
    /** `[JavaScript]` Extracts the InstanceType from the given Constructor type */
    InstanceType<T extends TConstructor<any[], any>>(schema: T, options?: SchemaOptions): TInstanceType<T>;
    /** `[JavaScript]` Creates an Iterator type */
    Iterator<T extends TSchema>(items: T, options?: SchemaOptions): TIterator<T>;
    /** `[JavaScript]` Extracts the Parameters from the given Function type */
    Parameters<T extends TFunction<TSchema[], TSchema>>(schema: T, options?: SchemaOptions): TParameters<T>;
    /** `[JavaScript]` Creates a Promise type */
    Promise<T extends TSchema>(item: T, options?: SchemaOptions): TPromise<T>;
    /** `[JavaScript]` Creates a String type from a Regular Expression pattern */
    RegExp(pattern: string, options?: SchemaOptions): TString;
    /** `[JavaScript]` Creates a String type from a Regular Expression */
    RegExp(regex: RegExp, options?: SchemaOptions): TString;
    /**
     * @deprecated Use `Type.RegExp`
     */
    RegEx(regex: RegExp, options?: SchemaOptions): TString;
    /** `[JavaScript]` Extracts the ReturnType from the given Function type */
    ReturnType<T extends TFunction<any[], any>>(schema: T, options?: SchemaOptions): TReturnType<T>;
    /** `[JavaScript]` Creates a Symbol type */
    Symbol(options?: SchemaOptions): TSymbol;
    /** `[JavaScript]` Creates a Undefined type */
    Undefined(options?: SchemaOptions): TUndefined;
    /** `[JavaScript]` Creates a Uint8Array type */
    Uint8Array(options?: Uint8ArrayOptions): TUint8Array;
    /** `[JavaScript]` Creates a Void type */
    Void(options?: SchemaOptions): TVoid;
}
/** Json Type Builder with Static Resolution for TypeScript */
export declare const JsonType: JsonTypeBuilder;
/** JavaScript Type Builder with Static Resolution for TypeScript */
export declare const Type: JavaScriptTypeBuilder;
