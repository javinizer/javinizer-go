type VoidFn = () => void;
type Subscriber = (update: VoidFn) => void | VoidFn;
export type Box<T> = {
    current: T;
};
export declare class ReactiveValue<T> implements Box<T> {
    #private;
    constructor(fn: () => T, onSubscribe: Subscriber);
    get current(): T;
}
/**
 * Makes all of the top-level keys of an object into $state.raw fields whose initial values
 * are the same as in the original object. Does not mutate the original object. Provides an `update`
 * function that _can_ (but does not have to be) be used to replace all of the object's top-level keys
 * with the values of the new object, while maintaining the original root object's reference.
 */
export declare function createRawRef<T extends {} | Array<unknown>>(init: T): [T, (newValue: T) => void];
export {};
//# sourceMappingURL=containers.svelte.d.ts.map