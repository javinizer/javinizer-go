import type { SettingsConfig } from '$lib/api/types';

/** Coarse display status for a field, driving the row's visual indicator. */
export type FieldStatus = 'inherited' | 'custom';

/** Global scraper execution priority from config. */
export function getGlobalPriority(config: SettingsConfig | undefined | null): string[] {
	return config?.scrapers?.priority ?? [];
}

/**
 * Resolve a field's effective scraper priority.
 *
 * Pure-exclusivity contract (no skip sentinel) — the three field states:
 *   - key ABSENT (or null/undefined) → inherit the global priority list
 *   - key present = []                → consult NO scrapers (field left empty)
 *   - key present = [a,b]             → consult a then b exclusively, no fallback
 *
 * A PRESENT empty array is a deliberate empty field, NOT an inherit signal —
 * this is what makes "Remove all" + Save persist an empty field. Only an ABSENT
 * (or null) key inherits the global list. `[]` and `undefined` are distinct.
 */
export function getFieldPriority(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
): string[] {
	const fields = config?.metadata?.priority;
	if (fields) {
		const v = fields[fieldKey];
		if (v !== undefined && v !== null) {
			return v; // present (incl. []) — [] means "no scrapers"
		}
	}
	return getGlobalPriority(config);
}

/**
 * Whether a field has a custom (non-inherited) override.
 *
 * A PRESENT value (including `[]`) that differs from the global list is an
 * override → true. A present `[]` differs from a non-empty global, so it counts
 * as overridden (custom). An ABSENT key (or null) → false (inherit global).
 * When the global list itself is empty, a present `[]` equals it and is NOT an
 * override (an empty field with an empty global is indistinguishable from
 * inherited, which is correct).
 */
export function isFieldOverridden(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
): boolean {
	const fields = config?.metadata?.priority;
	if (!fields) return false;
	const v = fields[fieldKey];
	if (v === undefined || v === null) return false; // absent/null → inherit
	return JSON.stringify(v) !== JSON.stringify(getGlobalPriority(config));
}

/**
 * Display status for a field, driving the row's visual indicator:
 * - "inherited" (green): no override, uses global priority.
 * - "custom" (orange): an exclusive override listing scrapers (possibly fewer
 *   than the global list — the user removed some for this field).
 */
export function getFieldStatus(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
): FieldStatus {
	return isFieldOverridden(config, fieldKey) ? 'custom' : 'inherited';
}

/**
 * Build a config mutation that sets a field's per-field priority override.
 *
 * Returns a new `metadata.priority` record (does not mutate the input). The
 * config shape encodes the three field states directly (no skip sentinel):
 *   - priority deep-equals global → DELETE the key (inherit = key ABSENT).
 *     Do NOT write `[]` — a present `[]` means "empty field", not "inherit".
 *   - priority differs from global (incl. `[]` when global is non-empty) →
 *     store it verbatim. A deliberate empty list stores `[]` (present-empty).
 */
export function buildFieldPriorityOverride(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
	priority: string[],
): Record<string, string[]> {
	const base = { ...(config?.metadata?.priority ?? {}) };
	const global = getGlobalPriority(config);
	if (JSON.stringify(priority) === JSON.stringify(global)) {
		delete base[fieldKey]; // inherit = key absent (NOT [])
	} else {
		base[fieldKey] = [...priority]; // differs — incl. deliberate-empty []
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
