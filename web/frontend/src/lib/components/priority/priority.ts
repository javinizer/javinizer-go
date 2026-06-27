import type { SettingsConfig } from '$lib/api/types';

/**
 * Reserved scraper name used to mark a metadata field as "do not scrape".
 *
 * When a field's per-field priority override is `["__skip__"]`, the backend
 * aggregator (PR #51, exclusive semantics) consults ONLY this entry. Because no
 * scraper named "__skip__" is ever registered or run, no result matches and the
 * field is left empty — an explicit, first-class way to suppress a field.
 *
 * This was verified to work with the existing backend (no backend change
 * required): `PriorityConfig.Fields` stores raw strings with no registry
 * validation, and the aggregator's priority loop simply finds no match.
 */
export const SKIP_FIELD_SENTINEL = '__skip__';

/** Coarse display status for a field, driving the row's visual indicator. */
export type FieldStatus = 'inherited' | 'custom' | 'skipped';

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
 * A non-empty override (including `["__skip__"]`) is returned as-is — under the
 * backend's exclusive semantics (#50), ONLY the listed scrapers are consulted
 * for that field, with NO fallback to the global list.
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
 * True when a priority list is the skip sentinel — i.e. the field is explicitly
 * suppressed and will be left empty.
 */
export function isSkipField(priority: string[] | undefined | null): boolean {
	return (
		Array.isArray(priority) && priority.length === 1 && priority[0] === SKIP_FIELD_SENTINEL
	);
}

/**
 * Whether a field has a custom (non-inherited) override.
 *
 * `[]`/undefined → false (inherit global). Any non-empty override that differs
 * from the global list → true. The skip sentinel counts as overridden (it is an
 * explicit override), but callers usually want {@link getFieldStatus} to
 * distinguish "skipped" from a regular custom override.
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
 * - "skipped" (grey): override is the `["__skip__"]` sentinel — field suppressed.
 * - "custom" (orange): an exclusive override listing real scrapers.
 *
 * "skipped" takes precedence over "custom" so the suppressed state is always
 * surfaced (a skip is technically also an override).
 */
export function getFieldStatus(
	config: SettingsConfig | undefined | null,
	fieldKey: string
): FieldStatus {
	const priority = config?.metadata?.priority?.[fieldKey];
	if (isSkipField(priority)) return 'skipped';
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
