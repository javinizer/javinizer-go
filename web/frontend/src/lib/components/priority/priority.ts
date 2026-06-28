import type { SettingsConfig } from '$lib/api/types';

/**
 * Canonical skip sentinel (slice form `["__skip__"]`).
 *
 * Under World A semantics, a present-empty `[]` field override means INHERIT
 * the global priority (folding the legacy World-B suppression form on read).
 * The only encoding for deliberate suppression is `["__skip__"]` — a sentinel
 * that matches no real scraper, so the field is left empty when consulted.
 */
export const SKIP_SENTINEL = '__skip__';

/** Whether a stored override list is the skip sentinel (`["__skip__"]`). */
export function isSkipSentinel(list: string[]): boolean {
	return list.length === 1 && list[0] === SKIP_SENTINEL;
}

/** Coarse display status for a field, driving the row's visual indicator. */
export type FieldStatus = 'inherited' | 'skipped' | 'custom';

/** Global scraper execution priority from config. */
export function getGlobalPriority(config: SettingsConfig | undefined | null): string[] {
	return config?.scrapers?.priority ?? [];
}

/**
 * Resolve a field's effective scraper priority.
 *
 * World A semantics — the three (technically four) field states:
 *   - key ABSENT (or null/undefined)          → inherit the global list
 *   - key present = ["__skip__"]               → skipped (field left empty;
 *                                               suppression sentinel)
 *   - key present = []                          → inherit global (legacy World-B
 *                                               form folded to inherit on read,
 *                                               matching the backend)
 *   - key present = [a,b] (any other list)     → consult a then b exclusively
 *
 * `["__skip__"]` is the only suppression encoding. A legacy present `[]` is
 * folded to inherit so the UI never crashes and matches the backend's
 * task-2 ([]) = inherit semantics.
 */
export function getFieldPriority(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
): string[] {
	const fields = config?.metadata?.priority;
	if (fields) {
		const v = fields[fieldKey];
		if (v !== undefined && v !== null) {
			// Legacy present-empty [] folds to inherit (World A). The skip sentinel
			// and any other list are returned verbatim.
			if (Array.isArray(v) && v.length === 0) {
				return getGlobalPriority(config);
			}
			return v;
		}
	}
	return getGlobalPriority(config);
}

/**
 * Whether a field has a custom (non-inherited) override.
 *
 * Both 'custom' (`[a,b]` differs from global) and 'skipped' (`["__skip__"]`)
 * are overrides of inherited → true. A present `[]` legacy form folds to
 * inherit on read, so a stored `[]` equal to an empty global is NOT an
 * override; a stored `[]` differing from a non-empty global is treated as
 * inherited (since `getFieldPriority` folds it to global). Only the skip
 * sentinel and genuinely custom lists count as overrides.
 */
export function isFieldOverridden(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
): boolean {
	const fields = config?.metadata?.priority;
	if (!fields) return false;
	const v = fields[fieldKey];
	if (v === undefined || v === null) return false; // absent/null → inherit
	if (isSkipSentinel(v)) return true; // skipped → overridden
	if (Array.isArray(v) && v.length === 0) return false; // legacy [] → inherit
	return JSON.stringify(v) !== JSON.stringify(getGlobalPriority(config));
}

/**
 * Display status for a field, driving the row's visual indicator:
 * - "inherited" (green): no override, uses the global priority list.
 * - "skipped"   (red/slate): suppressed via the `["__skip__"]` sentinel — the
 *   field is left empty (no scrapers consulted).
 * - "custom"   (orange): an exclusive override listing scrapers (possibly fewer
 *   than the global list — the user removed some for this field).
 */
export function getFieldStatus(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
): FieldStatus {
	const fields = config?.metadata?.priority;
	if (!fields) return 'inherited';
	const v = fields[fieldKey];
	if (v === undefined || v === null) return 'inherited';
	if (isSkipSentinel(v)) return 'skipped';
	if (Array.isArray(v) && v.length === 0) return 'inherited'; // legacy [] folds to inherit
	return JSON.stringify(v) !== JSON.stringify(getGlobalPriority(config))
		? 'custom'
		: 'inherited';
}

/**
 * Build a config mutation that sets a field's per-field priority override.
 *
 * Returns a new `metadata.priority` record (does not mutate the input). Under
 * World A semantics:
 *   - priority deep-equals global → DELETE the key (inherit = key ABSENT).
 *     Do NOT write `[]` — a present `[]` is LEGACY and folds to inherit on
 *     read (matching the backend), so writing it would mean inherit anyway.
 *   - priority is EMPTY (Remove all) → store `["__skip__"]` (the skip sentinel).
 *     This is the key behavioral change: "Remove all" + Save now stores the
 *     skip sentinel, NOT `[]`, so the field is deliberately suppressed rather
 *     than silently treated as inherit.
 *   - priority differs from global (non-empty override) → store it verbatim.
 */
export function buildFieldPriorityOverride(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
	priority: string[],
): Record<string, string[]> {
	const base = { ...(config?.metadata?.priority ?? {}) };
	const global = getGlobalPriority(config);
	if (priority.length === 0) {
		base[fieldKey] = [SKIP_SENTINEL]; // "Remove all" → suppress (checked first so an empty
		// list always persists the sentinel even when global is itself empty —
		// otherwise deep-equal-to-global would silently delete the key and the
		// field would re-inherit if global scrapers are added later)
	} else if (JSON.stringify(priority) === JSON.stringify(global)) {
		delete base[fieldKey]; // inherit = key absent (NOT [])
	} else {
		base[fieldKey] = [...priority]; // differs — store verbatim
	}
	return base;
}

/**
 * Apply an enabled-only reordering back onto the full stored priority list,
 * preserving any disabled scrapers that the DraggableList hid from display.
 *
 * The editor's DraggableList shows only enabled scrapers (it filters disabled
 * ones out of its `items` prop), so when the user reorders, `onReorder` yields
 * the new order of JUST the enabled scrapers. Writing that enabled-only list
 * directly back into the stored per-field priority would silently drop every
 * disabled scraper — and saving the narrowed override would persist the loss, so
 * re-enabling a disabled scraper later would not restore it for this field.
 *
 * To preserve them, the disabled scrapers (everything in `fullPriority` that
 * is not in `newEnabledOrder`) are kept in the list, appended after the enabled
 * ones in their original relative order. Disabled scrapers aren't draggable, so
 * their position relative to the enabled ones isn't user-meaningful; appending
 * them preserves both their presence and relative order without distorting the
 * enabled order the user just chose.
 *
 * Invariant: the returned list contains every scraper present in `fullPriority`
 * (none are dropped), with the enabled ones reordered to `newEnabledOrder`.
 *
 * @param fullPriority The full stored priority (enabled + disabled) — source of truth.
 * @param newEnabledOrder The reordered enabled-only list emitted by the DraggableList.
 * @returns The reconstructed full list: enabled scrapers in their new order,
 *         followed by the disabled scrapers in their original relative order.
 */
// applyEnabledReorderToFull applies a user's reorder of the enabled scrapers back
// onto the full stored priority list, preserving disabled scrapers (in relative
// order) at the end. `fullPriority` is the source of truth: `newEnabledOrder` is
// filtered against it so stale/unknown scraper ids coming back from the UI
// cannot leak into the persisted override (CodeRabbit, PR #51).
export function applyEnabledReorderToFull(
	fullPriority: string[],
	newEnabledOrder: string[],
): string[] {
	const allowed = new Set(fullPriority);
	const reorderedEnabled = newEnabledOrder.filter((name) => allowed.has(name));
	const enabledSet = new Set(reorderedEnabled);
	const disabled = fullPriority.filter((name) => !enabledSet.has(name));
	return [...reorderedEnabled, ...disabled];
}
