import { TypeBoxError, TSchema, TRef, TThis } from '../typebox';
export declare class TypeDereferenceError extends TypeBoxError {
    readonly schema: TRef | TThis;
    constructor(schema: TRef | TThis);
}
/** Dereferences a schema from the references array or throws if not found */
export declare function Deref(schema: TRef | TThis, references: TSchema[]): TSchema;
