var __classPrivateFieldGet = (this && this.__classPrivateFieldGet) || function (receiver, state, kind, f) {
    if (kind === "a" && !f) throw new TypeError("Private accessor was defined without a getter");
    if (typeof state === "function" ? receiver !== state || !f : !state.has(receiver)) throw new TypeError("Cannot read private member from an object whose class did not declare it");
    return kind === "m" ? f : kind === "a" ? f.call(receiver) : f ? f.value : state.get(receiver);
};
var __classPrivateFieldSet = (this && this.__classPrivateFieldSet) || function (receiver, state, value, kind, f) {
    if (kind === "m") throw new TypeError("Private method is not writable");
    if (kind === "a" && !f) throw new TypeError("Private accessor was defined without a setter");
    if (typeof state === "function" ? receiver !== state || !f : !state.has(receiver)) throw new TypeError("Cannot write private member to an object whose class did not declare it");
    return (kind === "a" ? f.call(receiver, value) : f ? f.value = value : state.set(receiver, value)), value;
};
var _ConnectionMutex_promise, _ConnectionMutex_resolve;
export class ConnectionMutex {
    constructor() {
        _ConnectionMutex_promise.set(this, void 0);
        _ConnectionMutex_resolve.set(this, void 0);
    }
    async lock() {
        while (__classPrivateFieldGet(this, _ConnectionMutex_promise, "f")) {
            await __classPrivateFieldGet(this, _ConnectionMutex_promise, "f");
        }
        __classPrivateFieldSet(this, _ConnectionMutex_promise, new Promise((resolve) => {
            __classPrivateFieldSet(this, _ConnectionMutex_resolve, resolve, "f");
        }), "f");
    }
    unlock() {
        const resolve = __classPrivateFieldGet(this, _ConnectionMutex_resolve, "f");
        __classPrivateFieldSet(this, _ConnectionMutex_promise, undefined, "f");
        __classPrivateFieldSet(this, _ConnectionMutex_resolve, undefined, "f");
        resolve?.();
    }
}
_ConnectionMutex_promise = new WeakMap(), _ConnectionMutex_resolve = new WeakMap();
//# sourceMappingURL=ConnectionMutex.js.map