var __classPrivateFieldSet = (this && this.__classPrivateFieldSet) || function (receiver, state, value, kind, f) {
    if (kind === "m") throw new TypeError("Private method is not writable");
    if (kind === "a" && !f) throw new TypeError("Private accessor was defined without a setter");
    if (typeof state === "function" ? receiver !== state || !f : !state.has(receiver)) throw new TypeError("Cannot write private member to an object whose class did not declare it");
    return (kind === "a" ? f.call(receiver, value) : f ? f.value = value : state.set(receiver, value)), value;
};
var __classPrivateFieldGet = (this && this.__classPrivateFieldGet) || function (receiver, state, kind, f) {
    if (kind === "a" && !f) throw new TypeError("Private accessor was defined without a getter");
    if (typeof state === "function" ? receiver !== state || !f : !state.has(receiver)) throw new TypeError("Cannot read private member from an object whose class did not declare it");
    return kind === "m" ? f : kind === "a" ? f.call(receiver) : f ? f.value : state.get(receiver);
};
var _SqliteWasmDriver_config, _SqliteWasmDriver_connectionMutex, _SqliteWasmDriver_db, _SqliteWasmDriver_connection;
import { CompiledQuery } from "kysely";
import { ConnectionMutex } from "./ConnectionMutex.js";
import { SqliteWasmConnection } from "./SqliteWasmConnection.js";
export class SqliteWasmDriver {
    constructor(config) {
        _SqliteWasmDriver_config.set(this, void 0);
        _SqliteWasmDriver_connectionMutex.set(this, new ConnectionMutex());
        _SqliteWasmDriver_db.set(this, void 0);
        _SqliteWasmDriver_connection.set(this, void 0);
        // this.#config = freeze({ ...config })
        __classPrivateFieldSet(this, _SqliteWasmDriver_config, { ...config }, "f");
    }
    async init() {
        __classPrivateFieldSet(this, _SqliteWasmDriver_db, typeof __classPrivateFieldGet(this, _SqliteWasmDriver_config, "f").database === "function"
            ? await __classPrivateFieldGet(this, _SqliteWasmDriver_config, "f").database()
            : __classPrivateFieldGet(this, _SqliteWasmDriver_config, "f").database, "f");
        __classPrivateFieldSet(this, _SqliteWasmDriver_connection, new SqliteWasmConnection(__classPrivateFieldGet(this, _SqliteWasmDriver_db, "f")), "f");
        if (__classPrivateFieldGet(this, _SqliteWasmDriver_config, "f").onCreateConnection) {
            await __classPrivateFieldGet(this, _SqliteWasmDriver_config, "f").onCreateConnection(__classPrivateFieldGet(this, _SqliteWasmDriver_connection, "f"));
        }
    }
    async acquireConnection() {
        // SQLite only has one single connection. We use a mutex here to wait
        // until the single connection has been released.
        await __classPrivateFieldGet(this, _SqliteWasmDriver_connectionMutex, "f").lock();
        return __classPrivateFieldGet(this, _SqliteWasmDriver_connection, "f");
    }
    async beginTransaction(connection) {
        await connection.executeQuery(CompiledQuery.raw("begin"));
    }
    async commitTransaction(connection) {
        await connection.executeQuery(CompiledQuery.raw("commit"));
    }
    async rollbackTransaction(connection) {
        await connection.executeQuery(CompiledQuery.raw("rollback"));
    }
    async releaseConnection() {
        __classPrivateFieldGet(this, _SqliteWasmDriver_connectionMutex, "f").unlock();
    }
    async destroy() {
        __classPrivateFieldGet(this, _SqliteWasmDriver_db, "f")?.close();
    }
}
_SqliteWasmDriver_config = new WeakMap(), _SqliteWasmDriver_connectionMutex = new WeakMap(), _SqliteWasmDriver_db = new WeakMap(), _SqliteWasmDriver_connection = new WeakMap();
//# sourceMappingURL=SqliteWasmDriver.js.map