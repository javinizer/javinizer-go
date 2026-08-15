import type { Kysely } from "kysely";
import type { InlangDatabaseSchema } from "../database/schema.js";
/**
 * Select bundles with nested messages and variants.
 *
 * `{ bundle, messages: [{ message, variants: [{ variant }] }] }`
 *
 * @example
 *   // getting one bundle where id is 123
 *   await selectBundleNested(db)
 *     .where("bundle.id", "=", "123")
 *     .executeTakeFirst()
 *
 *   // getting all bundles
 *   await selectBundleNested(db)
 *     .execute()
 */
export declare const selectBundleNested: (db: Kysely<InlangDatabaseSchema>) => import("kysely").SelectQueryBuilder<InlangDatabaseSchema, "bundle", {
    id: string;
    declarations: ({
        type: "local-variable";
        value: {
            annotation?: {
                type: "function-reference";
                name: string;
                options: {
                    value: {
                        type: "variable-reference";
                        name: string;
                    } | {
                        type: "literal";
                        value: string;
                    };
                    name: string;
                }[];
            } | undefined;
            type: "expression";
            arg: {
                type: "variable-reference";
                name: string;
            } | {
                type: "literal";
                value: string;
            };
        };
        name: string;
    } | {
        annotation?: {
            type: "function-reference";
            name: string;
            options: {
                value: {
                    type: "variable-reference";
                    name: string;
                } | {
                    type: "literal";
                    value: string;
                };
                name: string;
            }[];
        } | undefined;
        type: "input-variable";
        name: string;
    })[];
    messages: {
        id: string;
        bundleId: string;
        locale: string;
        selectors: {
            type: "variable-reference";
            name: string;
        }[];
        variants: {
            pattern: ({
                annotation?: {
                    type: "function-reference";
                    name: string;
                    options: {
                        value: {
                            type: "variable-reference";
                            name: string;
                        } | {
                            type: "literal";
                            value: string;
                        };
                        name: string;
                    }[];
                } | undefined;
                type: "expression";
                arg: {
                    type: "variable-reference";
                    name: string;
                } | {
                    type: "literal";
                    value: string;
                };
            } | {
                type: "text";
                value: string;
            } | {
                options?: {
                    value: {
                        type: "variable-reference";
                        name: string;
                    } | {
                        type: "literal";
                        value: string;
                    };
                    name: string;
                }[] | undefined;
                attributes?: {
                    value: true | {
                        type: "literal";
                        value: string;
                    };
                    name: string;
                }[] | undefined;
                type: "markup-start";
                name: string;
            } | {
                options?: {
                    value: {
                        type: "variable-reference";
                        name: string;
                    } | {
                        type: "literal";
                        value: string;
                    };
                    name: string;
                }[] | undefined;
                attributes?: {
                    value: true | {
                        type: "literal";
                        value: string;
                    };
                    name: string;
                }[] | undefined;
                type: "markup-end";
                name: string;
            } | {
                options?: {
                    value: {
                        type: "variable-reference";
                        name: string;
                    } | {
                        type: "literal";
                        value: string;
                    };
                    name: string;
                }[] | undefined;
                attributes?: {
                    value: true | {
                        type: "literal";
                        value: string;
                    };
                    name: string;
                }[] | undefined;
                type: "markup-standalone";
                name: string;
            })[];
            id: string;
            messageId: string;
            matches: import("../database/schema.js").Match[];
        }[];
    }[];
}>;
//# sourceMappingURL=selectBundleNested.d.ts.map