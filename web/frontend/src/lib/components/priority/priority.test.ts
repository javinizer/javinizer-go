import { describe, it, expect } from 'vitest';
import {
	SKIP_FIELD_SENTINEL,
	getGlobalPriority,
	getFieldPriority,
	isSkipField,
	isFieldOverridden,
	getFieldStatus,
	buildFieldPriorityOverride,
	applyEnabledReorderToFull
} from './priority';
import type { SettingsConfig, ScraperSettings } from '$lib/api/types';

/**
 * Minimal config builder. The priority helpers only read `scrapers.priority`
 * and `metadata.priority`, so we cast a partial object to satisfy the
 * (fully-required) SettingsConfig type without constructing every section.
 */
function makeConfig(
	opts: { global?: string[]; fields?: Record<string, string[]> } = {}
): SettingsConfig {
	return {
		scrapers: { priority: opts.global ?? [] },
		metadata: { priority: opts.fields ?? {} }
	} as unknown as SettingsConfig;
}

/**
 * Config builder that can mark specific scrapers as disabled.
 *
 * `config.scrapers.priority` lists every scraper (enabled + disabled); a scraper
 * is disabled by `config.scrapers[name].enabled = false`. Only `enabled` is read
 * by the code under test, so the rest of the ScraperSettings shape is omitted
 * (the whole object is cast to satisfy the fully-required SettingsConfig type,
 * mirroring makeConfig above).
 */
function makeConfigWithScrapers(
	opts: { global?: string[]; fields?: Record<string, string[]>; disabled?: string[] } = {}
): SettingsConfig {
	const scrapers: Record<string, { enabled: boolean }> = {};
	for (const name of opts.disabled ?? []) {
		scrapers[name] = { enabled: false };
	}
	return {
		scrapers: { priority: opts.global ?? [], ...scrapers },
		metadata: { priority: opts.fields ?? {} }
	} as unknown as SettingsConfig;
}

describe('priority: sentinel', () => {
	it('exposes the __skip__ sentinel constant', () => {
		expect(SKIP_FIELD_SENTINEL).toBe('__skip__');
	});

	it('isSkipField is true only for the single-element ["__skip__"] list', () => {
		expect(isSkipField([SKIP_FIELD_SENTINEL])).toBe(true);
		expect(isSkipField(['__skip__'])).toBe(true);
		expect(isSkipField(['r18dev'])).toBe(false);
		expect(isSkipField(['r18dev', 'dmm'])).toBe(false);
		expect(isSkipField([])).toBe(false);
		expect(isSkipField(undefined)).toBe(false);
		expect(isSkipField(null)).toBe(false);
	});
});

describe('priority: getGlobalPriority', () => {
	it('returns the global scraper priority list', () => {
		expect(getGlobalPriority(makeConfig({ global: ['r18dev', 'dmm'] }))).toEqual([
			'r18dev',
			'dmm'
		]);
	});

	it('returns [] when unset or when config is nil', () => {
		expect(getGlobalPriority(makeConfig())).toEqual([]);
		expect(getGlobalPriority(undefined)).toEqual([]);
		expect(getGlobalPriority(null)).toEqual([]);
	});
});

describe('priority: getFieldPriority', () => {
	it('returns global for an undefined field override (inherit)', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'] });
		expect(getFieldPriority(config, 'series')).toEqual(['r18dev', 'dmm']);
	});

	it('returns global for an empty-array override ([] means inherit, not skip)', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { series: [] } });
		expect(getFieldPriority(config, 'series')).toEqual(['r18dev', 'dmm']);
	});

	it('returns the exclusive override when set (no global fallback)', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { series: ['tokyohot'] } });
		expect(getFieldPriority(config, 'series')).toEqual(['tokyohot']);
	});

	it('preserves the __skip__ sentinel as the resolved priority', () => {
		const config = makeConfig({
			global: ['r18dev', 'dmm'],
			fields: { series: [SKIP_FIELD_SENTINEL] }
		});
		expect(getFieldPriority(config, 'series')).toEqual([SKIP_FIELD_SENTINEL]);
	});
});

describe('priority: isFieldOverridden', () => {
	it('returns false for [] and undefined', () => {
		expect(isFieldOverridden(makeConfig({ global: ['r18dev'] }), 'series')).toBe(false);
		expect(
			isFieldOverridden(makeConfig({ global: ['r18dev'], fields: { series: [] } }), 'series')
		).toBe(false);
	});

	it('returns true for a non-empty override that differs from global', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { series: ['tokyohot'] } });
		expect(isFieldOverridden(config, 'series')).toBe(true);
	});

	it('returns false when the override equals global (redundant ⇒ treated as inherited)', () => {
		const config = makeConfig({
			global: ['r18dev', 'dmm'],
			fields: { series: ['r18dev', 'dmm'] }
		});
		expect(isFieldOverridden(config, 'series')).toBe(false);
	});

	it('returns true for the __skip__ sentinel (it is an explicit override)', () => {
		const config = makeConfig({
			global: ['r18dev', 'dmm'],
			fields: { series: [SKIP_FIELD_SENTINEL] }
		});
		expect(isFieldOverridden(config, 'series')).toBe(true);
	});
});

describe('priority: getFieldStatus', () => {
	it('is "inherited" with no override', () => {
		expect(getFieldStatus(makeConfig({ global: ['r18dev'] }), 'series')).toBe('inherited');
	});

	it('is "inherited" for []', () => {
		expect(
			getFieldStatus(makeConfig({ global: ['r18dev'], fields: { series: [] } }), 'series')
		).toBe('inherited');
	});

	it('is "custom" for a non-empty exclusive override', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { series: ['tokyohot'] } });
		expect(getFieldStatus(config, 'series')).toBe('custom');
	});

	it('is "skipped" for the __skip__ sentinel (precedence over "custom")', () => {
		const config = makeConfig({
			global: ['r18dev', 'dmm'],
			fields: { series: [SKIP_FIELD_SENTINEL] }
		});
		expect(getFieldStatus(config, 'series')).toBe('skipped');
	});
});

describe('priority: buildFieldPriorityOverride (skip-action config shape)', () => {
	it('stores the __skip__ sentinel when skipping a field', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'] });
		const next = buildFieldPriorityOverride(config, 'series', [SKIP_FIELD_SENTINEL]);
		expect(next.series).toEqual([SKIP_FIELD_SENTINEL]);
	});

	it('preserves other field overrides when setting a new one', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { genre: ['dmm'] } });
		const next = buildFieldPriorityOverride(config, 'series', [SKIP_FIELD_SENTINEL]);
		expect(next.genre).toEqual(['dmm']);
		expect(next.series).toEqual([SKIP_FIELD_SENTINEL]);
	});

	it('collapses a global-equal override to [] (inherit)', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'] });
		const next = buildFieldPriorityOverride(config, 'series', ['r18dev', 'dmm']);
		expect(next.series).toEqual([]);
	});

	it('does not mutate the original config', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { genre: ['dmm'] } });
		const snapshot = JSON.parse(JSON.stringify(config.metadata.priority));
		buildFieldPriorityOverride(config, 'series', [SKIP_FIELD_SENTINEL]);
		expect(JSON.parse(JSON.stringify(config.metadata.priority))).toEqual(snapshot);
	});
});

describe('priority: applyEnabledReorderToFull (disabled-scraper preservation)', () => {
	it('appends disabled scrapers after the reordered enabled ones (none dropped)', () => {
		// full stored list has 'javbus' disabled (hidden by the DraggableList)
		const full = ['r18dev', 'dmm', 'javbus'];
		// DraggableList shows only enabled; user reorders [r18dev, dmm] -> [dmm, r18dev]
		const newEnabledOrder = ['dmm', 'r18dev'];
		expect(applyEnabledReorderToFull(full, newEnabledOrder)).toEqual([
			'dmm',
			'r18dev',
			'javbus'
		]);
	});

	it('preserves the relative order of multiple disabled scrapers', () => {
		const full = ['r18dev', 'dmm', 'javbus', 'javdb'];
		// enabled = [r18dev, dmm]; disabled = [javbus, javdb] (kept in original order)
		const newEnabledOrder = ['dmm', 'r18dev'];
		expect(applyEnabledReorderToFull(full, newEnabledOrder)).toEqual([
			'dmm',
			'r18dev',
			'javbus',
			'javdb'
		]);
	});

	it('is a plain reorder when there are no disabled scrapers', () => {
		const full = ['r18dev', 'dmm'];
		const newEnabledOrder = ['dmm', 'r18dev'];
		expect(applyEnabledReorderToFull(full, newEnabledOrder)).toEqual(['dmm', 'r18dev']);
	});

	it('never drops a scraper present in the full list (invariant)', () => {
		const full = ['r18dev', 'dmm', 'javbus', 'javdb', 'jav321'];
		const newEnabledOrder = ['dmm', 'r18dev'];
		const result = applyEnabledReorderToFull(full, newEnabledOrder);
		// every scraper from the full list survives the reorder
		for (const name of full) {
			expect(result).toContain(name);
		}
		expect(result).toHaveLength(full.length);
	});

	it('filters stale/unknown scraper ids from newEnabledOrder (source of truth = fullPriority)', () => {
		// CodeRabbit (PR #51): newEnabledOrder is trusted verbatim, so a stale or
		// unknown id coming back from the UI would leak into the persisted override.
		// fullPriority is the source of truth; only ids present in it are kept.
		const full = ['r18dev', 'dmm', 'javbus'];
		const newEnabledOrder = ['dmm', 'GHOST_STALE_ID', 'r18dev'];
		expect(applyEnabledReorderToFull(full, newEnabledOrder)).toEqual([
			'dmm',
			'r18dev',
			'javbus'
		]);
	});
});

describe('priority: editor data flow preserves disabled scrapers through reorder + save', () => {
	// Mirrors MetadataPriority.svelte's component-local filterEnabledScrapers:
	// the DraggableList shows only scrapers whose config entry isn't disabled.
	function enabledView(config: SettingsConfig, priority: string[]): string[] {
		return priority.filter(
			(name) =>
				(config.scrapers?.[name] as ScraperSettings | undefined)?.enabled !== false
		);
	}

	it('re-enable a skipped field + reorder + save keeps disabled scrapers in the override', () => {
		// Global priority lists 3 scrapers; 'javbus' is disabled.
		const config = makeConfigWithScrapers({
			global: ['r18dev', 'dmm', 'javbus'],
			disabled: ['javbus'],
			// field 'series' was previously skipped
			fields: { series: [SKIP_FIELD_SENTINEL] }
		});

		// 1) User re-enables the skipped field: enableField() now stages the FULL
		//    global list (enabled + disabled), NOT the enabled-only view.
		let editingPriority = [...getGlobalPriority(config)];
		expect(editingPriority).toEqual(['r18dev', 'dmm', 'javbus']);

		// 2) DraggableList shows only enabled scrapers; user reorders them.
		const view = enabledView(config, editingPriority); // ['r18dev', 'dmm']
		const newEnabledOrder = [...view].reverse(); // ['dmm', 'r18dev']
		editingPriority = applyEnabledReorderToFull(editingPriority, newEnabledOrder);

		// 3) Save: buildFieldPriorityOverride persists editingPriority (differs
		//    from global, so it is NOT collapsed to []). The disabled 'javbus'
		//    MUST survive — this is the CodeRabbit Finding 2 regression.
		const saved = buildFieldPriorityOverride(config, 'series', editingPriority);
		expect(saved.series).toEqual(['dmm', 'r18dev', 'javbus']);
		expect(saved.series).toContain('javbus'); // disabled scraper preserved
		expect(saved.series).not.toEqual(['dmm', 'r18dev']); // not narrowed to enabled-only
	});

	it('re-enable a skipped field + save (unchanged) restores "inherited" ([])', () => {
		const config = makeConfigWithScrapers({
			global: ['r18dev', 'dmm', 'javbus'],
			disabled: ['javbus'],
			fields: { series: [SKIP_FIELD_SENTINEL] }
		});

		// enableField stages the full global list (incl. disabled 'javbus').
		const editingPriority = [...getGlobalPriority(config)];

		// Save unchanged: editingPriority === global => buildFieldPriorityOverride
		// collapses to [] (inherited), even though 'javbus' is disabled.
		const saved = buildFieldPriorityOverride(config, 'series', editingPriority);
		expect(saved.series).toEqual([]);
	});

	it('reorder an inherited field + save keeps disabled scrapers in the override', () => {
		// An inherited field (no override) edited + reordered + saved.
		const config = makeConfigWithScrapers({
			global: ['r18dev', 'dmm', 'javbus'],
			disabled: ['javbus']
			// no fields override => 'series' inherits global
		});

		// openFieldEditor loads the full stored list (== global for inherited).
		let editingPriority = [...getFieldPriority(config, 'series')];
		const view = enabledView(config, editingPriority); // ['r18dev', 'dmm']
		const newEnabledOrder = [...view].reverse(); // ['dmm', 'r18dev']
		editingPriority = applyEnabledReorderToFull(editingPriority, newEnabledOrder);

		const saved = buildFieldPriorityOverride(config, 'series', editingPriority);
		expect(saved.series).toEqual(['dmm', 'r18dev', 'javbus']);
		expect(saved.series).toContain('javbus'); // disabled scraper preserved
	});
});
