"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.PostgresIntrospector = void 0;
const migrator_js_1 = require("../../migration/migrator.js");
const object_utils_js_1 = require("../../util/object-utils.js");
const sql_js_1 = require("../../raw-builder/sql.js");
class PostgresIntrospector {
    #db;
    constructor(db) {
        this.#db = db;
    }
    async getSchemas() {
        let rawSchemas = await this.#db
            .selectFrom('pg_catalog.pg_namespace')
            .select('nspname')
            .$castTo()
            .execute();
        return rawSchemas.map((it) => ({ name: it.nspname }));
    }
    async getTables(options = { withInternalKyselyTables: false }) {
        let query = this.#db
            // column
            .selectFrom('pg_catalog.pg_attribute as a')
            // table
            .innerJoin('pg_catalog.pg_class as c', 'a.attrelid', 'c.oid')
            // table schema
            .innerJoin('pg_catalog.pg_namespace as ns', 'c.relnamespace', 'ns.oid')
            // column data type
            .innerJoin('pg_catalog.pg_type as typ', 'a.atttypid', 'typ.oid')
            // column data type schema
            .innerJoin('pg_catalog.pg_namespace as dtns', 'typ.typnamespace', 'dtns.oid')
            .select([
            'a.attname as column',
            'a.attnotnull as not_null',
            'a.atthasdef as has_default',
            'c.relname as table',
            'c.relkind as table_type',
            'ns.nspname as schema',
            'typ.typname as type',
            'dtns.nspname as type_schema',
            (0, sql_js_1.sql) `col_description(a.attrelid, a.attnum)`.as('column_description'),
            (0, sql_js_1.sql) `pg_get_serial_sequence(quote_ident(ns.nspname) || '.' || quote_ident(c.relname), a.attname)`.as('auto_incrementing'),
        ])
            .where('c.relkind', 'in', [
            'r' /*regular table*/,
            'v' /*view*/,
            'p' /*partitioned table*/,
        ])
            .where('ns.nspname', '!~', '^pg_')
            .where('ns.nspname', '!=', 'information_schema')
            // Filter out internal cockroachdb schema
            .where('ns.nspname', '!=', 'crdb_internal')
            // Only schemas where we are allowed access
            .where((0, sql_js_1.sql) `has_schema_privilege(ns.nspname, 'USAGE')`)
            // No system columns
            .where('a.attnum', '>=', 0)
            .where('a.attisdropped', '!=', true)
            .orderBy('ns.nspname')
            .orderBy('c.relname')
            .orderBy('a.attnum')
            .$castTo();
        if (!options.withInternalKyselyTables) {
            query = query
                .where('c.relname', '!=', migrator_js_1.DEFAULT_MIGRATION_TABLE)
                .where('c.relname', '!=', migrator_js_1.DEFAULT_MIGRATION_LOCK_TABLE);
        }
        const rawColumns = await query.execute();
        return this.#parseTableMetadata(rawColumns);
    }
    async getMetadata(options) {
        return {
            tables: await this.getTables(options),
        };
    }
    #parseTableMetadata(columns) {
        const tableDictionary = new Map();
        for (let i = 0, len = columns.length; i < len; i++) {
            const column = columns[i];
            const { schema, table } = column;
            const tableKey = `schema:${schema};table:${table}`;
            if (!tableDictionary.has(tableKey)) {
                tableDictionary.set(tableKey, (0, object_utils_js_1.freeze)({
                    columns: [],
                    isView: column.table_type === 'v',
                    name: table,
                    schema,
                }));
            }
            tableDictionary.get(tableKey).columns.push((0, object_utils_js_1.freeze)({
                comment: column.column_description ?? undefined,
                dataType: column.type,
                dataTypeSchema: column.type_schema,
                hasDefaultValue: column.has_default,
                isAutoIncrementing: column.auto_incrementing !== null,
                isNullable: !column.not_null,
                name: column.column,
            }));
        }
        return Array.from(tableDictionary.values());
    }
}
exports.PostgresIntrospector = PostgresIntrospector;
