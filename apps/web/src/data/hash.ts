/**
 * The two parts of an address hash (`#/<path>?<query>`). The router, the scroll keeper, the leave guard and the screen
 * key all split a hash the same way, so the split is written once here.
 */

/** The path of an address hash, without `#` and without the query. */
export const pathOf = (hash: string): string => hash.replace(/^#/, "").split("?")[0] ?? "";

/** The query of an address hash, the part after `?` (empty when there is none). */
export const queryOf = (hash: string): string => hash.replace(/^#/, "").split("?")[1] ?? "";
