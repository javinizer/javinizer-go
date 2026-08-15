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
var _SqliteWasmConnection_db;
export class SqliteWasmConnection {
    constructor(db) {
        _SqliteWasmConnection_db.set(this, void 0);
        __classPrivateFieldSet(this, _SqliteWasmConnection_db, db, "f");
    }
    executeQuery(compiledQuery) {
        const { sql, parameters } = compiledQuery;
        const statementData = {
            rows: [],
            columns: [],
        };
        // we cant know what kind of query we are dealing with at that state - unless we switch to perpared statments
        // for now we collect all information required
        // save the changes before (total changes seems to be fast and worth the twoe extra round trips inspiration from https://github.com/WiseLibs/better-sqlite3/blob/254b8e93d78b1b03c9a2c777f4d304a0ea1530c6/src/objects/statement.lzz#L159)
        const totalChangesBefore = __classPrivateFieldGet(this, _SqliteWasmConnection_db, "f").changes(true);
        // execute the statement
        const rows = __classPrivateFieldGet(this, _SqliteWasmConnection_db, "f").exec({
            sql: sql,
            bind: parameters,
            returnValue: "resultRows",
            rowMode: "object",
            columnNames: statementData.columns,
        });
        const lastInsertId = __classPrivateFieldGet(this, _SqliteWasmConnection_db, "f").sqlite3.capi.sqlite3_last_insert_rowid(__classPrivateFieldGet(this, _SqliteWasmConnection_db, "f"));
        // check if we had changes in the db at all - if so - collect the number of changes
        const changes = totalChangesBefore === __classPrivateFieldGet(this, _SqliteWasmConnection_db, "f").changes(true) ? 0 : __classPrivateFieldGet(this, _SqliteWasmConnection_db, "f").changes();
        // console.log('sql: ' + sql);
        // console.log('result: ', rows);
        // We don't have knowledge about rather its update/delete/or select - so we return the results
        // @ts-expect-error - TODO for @martin-lysk - typescript complains
        return Promise.resolve({
            numAffectedRows: changes,
            insertId: lastInsertId,
            // queries with result
            rows: rows,
        });
    }
    // eslint-disable-next-line require-yield
    async *streamQuery() {
        throw new Error("not supported for wasm driver yet");
    }
}
_SqliteWasmConnection_db = new WeakMap();
//# sourceMappingURL=SqliteWasmConnection.js.map