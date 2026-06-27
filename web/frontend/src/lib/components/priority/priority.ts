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
 * Empty/undefined override → inherit the global priority list (the unchanged
 * "inherit" path — `[]` and `undefined` are treated identically, matching the
 * backend's `GetFieldPriority` which falls back to global for empty slices).
 *
 * A non-empty override is returned as-is — under the backend's exclusive
 * semantics (#50), ONLY the listed scrapers are consulted for that field, with
 * NO fallback to the global list. There is no skip sentinel: a field is left
 * empty by pointing it at a scraper that didn't run or lacks the field
 * (e.g. `series: [tokyohot]`), which is the original (v1) Javinizer behavior.
 */
export function getFieldPriority(
	config: SettingsConfig | undefined | null,
	fieldKey: string
): string[] {
	const fieldConfig = config?.metadata?.priority?.[fieldKey];
	if (!fieldConfig || fieldConfig.length === 0) {
		return getGlobalPriority(config);
	}
	return fieldConfig;
}

/**
 * Whether a field has a custom (non-inherited) override.
 *
 * `[]`/undefined → false (inherit global). Any non-empty override that differs
 * from the global list → true.
 */
export function isFieldOverridden(
	config: SettingsConfig | undefined | null,
	fieldKey: string
): boolean {
	const fieldConfig = config?.metadata?.priority?.[fieldKey];
	if (!fieldConfig || fieldConfig.length === 0) {
		return false;
	}
	return JSON.stringify(fieldConfig) !== JSON.stringify(getGlobalPriority(config));
}

/**
 * Display status for a field, driving the row's visual indicator:
 * - "inherited" (green): no override, uses global priority.
 * - "custom" (orange): an exclusive override listing scrapers (possibly fewer
 *   than the global list — the user removed some for this field).
 */
export function getFieldStatus(
	config: SettingsConfig | undefined | null,
	fieldKey: string
): FieldStatus {
	return isFieldOverridden(config, fieldKey) ? 'custom' : 'inherited';
}

/**
 * Build a config mutation that sets a field's per-field priority override.
 *
 * Returns a new `metadata.priority` record (does not mutate the input). When
 * the resolved priority equals the global list, an empty array is stored instead
 * so the field reads as "inherited" (matching the backend's `[]` ⇒ global
 * semantics and keeping the config free of redundant overrides).
 */
export function buildFieldPriorityOverride(
	config: SettingsConfig | undefined | null,
	fieldKey: string,
	priority: string[]
): Record<string, string[]> {
	const base = { ...(config?.metadata?.priority ?? {}) };
	const global = getGlobalPriority(config);
	if (JSON.stringify(priority) === JSON.stringify(global)) {
		base[fieldKey] = [];
	} else {
		base[fieldKey] = [...priority];
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
	newEnabledOrder: string[]
): string[] {
	const allowed = new Set(fullPriority);
	const reorderedEnabled = newEnabledOrder.filter((name) => allowed.has(name));
	const enabledSet = new Set(reorderedEnabled);
	const disabled = fullPriority.filter((name) => !enabledSet.has(name));
	return [...reorderedEnabled, ...disabled];
}
