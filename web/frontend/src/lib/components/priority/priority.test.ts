import { describe, it, expect } from 'vitest';
import {
	getGlobalPriority,
	getFieldPriority,
	isFieldOverridden,
	getFieldStatus,
	buildFieldPriorityOverride,
	applyEnabledReorderToFull,
	SKIP_SENTINEL,
	isSkipSentinel,
} from './priority';
import type { SettingsConfig, ScraperSettings } from '$lib/api/types';

/**
 * Minimal config builder. The priority helpers only read `scrapers.priority`
 * and `metadata.priority`, so we cast a partial object to satisfy the
 * (fully-required) SettingsConfig type without constructing every section.
 */
function makeConfig(
	opts: { global?: string[]; fields?: Record<string, string[]> } = {},
): SettingsConfig {
	return {
		scrapers: { priority: opts.global ?? [] },
		metadata: { priority: opts.fields ?? {} },
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
	opts: { global?: string[]; fields?: Record<string, string[]>; disabled?: string[] } = {},
): SettingsConfig {
	const scrapers: Record<string, { enabled: boolean }> = {};
	for (const name of opts.disabled ?? []) {
		scrapers[name] = { enabled: false };
	}
	return {
		scrapers: { priority: opts.global ?? [], ...scrapers },
		metadata: { priority: opts.fields ?? {} },
	} as unknown as SettingsConfig;
}

describe('priority: getGlobalPriority', () => {
	it('returns the global scraper priority list', () => {
		expect(getGlobalPriority(makeConfig({ global: ['r18dev', 'dmm'] }))).toEqual(['r18dev', 'dmm']);
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

	it('returns ["__skip__"] for the skip sentinel (suppression, NOT folded to global)', () => {
		// ["__skip__"] is the only suppression encoding. It must NOT fold to the
		// global list on read.
		const config = makeConfig({
			global: ['r18dev', 'dmm'],
			fields: { series: [SKIP_SENTINEL] },
		});
		expect(getFieldPriority(config, 'series')).toEqual([SKIP_SENTINEL]);
	});

	it('returns global for a present-empty legacy override (World A: [] folds to inherit)', () => {
		// A legacy present [] is folded to inherit on read, matching the backend.
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { series: [] } });
		expect(getFieldPriority(config, 'series')).toEqual(['r18dev', 'dmm']);
	});

	it('returns global for a present-null override (null ⇒ inherit)', () => {
		const config = makeConfig({
			global: ['r18dev', 'dmm'],
			fields: { series: null as unknown as string[] },
		});
		expect(getFieldPriority(config, 'series')).toEqual(['r18dev', 'dmm']);
	});

	it('returns the exclusive override when set (no global fallback)', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { series: ['tokyohot'] } });
		expect(getFieldPriority(config, 'series')).toEqual(['tokyohot']);
	});
});

describe('priority: isFieldOverridden', () => {
	it('returns false for undefined (absent ⇒ inherit)', () => {
		expect(isFieldOverridden(makeConfig({ global: ['r18dev'] }), 'series')).toBe(false);
	});

	it('returns false for a present-null override (null ⇒ inherit)', () => {
		expect(
			isFieldOverridden(
				makeConfig({ global: ['r18dev'], fields: { series: null as unknown as string[] } }),
				'series',
			),
		).toBe(false);
	});

	it('returns true for the skip sentinel (suppression counts as an override)', () => {
		expect(
			isFieldOverridden(
				makeConfig({ global: ['r18dev'], fields: { series: [SKIP_SENTINEL] } }),
				'series',
			),
		).toBe(true);
	});

	it('returns false for a present-empty legacy override (World A: [] folds to inherit)', () => {
		// A legacy present [] folds to inherit on read, so it is NOT an override.
		expect(
			isFieldOverridden(makeConfig({ global: ['r18dev'], fields: { series: [] } }), 'series'),
		).toBe(false);
	});

	it('returns true for a non-empty override that differs from global', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { series: ['tokyohot'] } });
		expect(isFieldOverridden(config, 'series')).toBe(true);
	});

	it('returns false when the override equals global (redundant ⇒ treated as inherited)', () => {
		const config = makeConfig({
			global: ['r18dev', 'dmm'],
			fields: { series: ['r18dev', 'dmm'] },
		});
		expect(isFieldOverridden(config, 'series')).toBe(false);
	});
});

describe('priority: getFieldStatus', () => {
	it('is "inherited" with no override', () => {
		expect(getFieldStatus(makeConfig({ global: ['r18dev'] }), 'series')).toBe('inherited');
	});

	it('is "skipped" for the skip sentinel (["__skip__"])', () => {
		expect(
			getFieldStatus(
				makeConfig({ global: ['r18dev'], fields: { series: [SKIP_SENTINEL] } }),
				'series',
			),
		).toBe('skipped');
	});

	it('is "inherited" for a present-empty legacy override (World A: [] folds to inherit)', () => {
		expect(
			getFieldStatus(makeConfig({ global: ['r18dev'], fields: { series: [] } }), 'series'),
		).toBe('inherited');
	});

	it('is "inherited" for a present-null override (null ⇒ inherit)', () => {
		expect(
			getFieldStatus(
				makeConfig({ global: ['r18dev'], fields: { series: null as unknown as string[] } }),
				'series',
			),
		).toBe('inherited');
	});

	it('is "custom" for a non-empty exclusive override', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { series: ['tokyohot'] } });
		expect(getFieldStatus(config, 'series')).toBe('custom');
	});
});

describe('priority: isSkipSentinel', () => {
	it('returns true for [SKIP_SENTINEL]', () => {
		expect(isSkipSentinel([SKIP_SENTINEL])).toBe(true);
	});

	it('returns false for an empty list', () => {
		expect(isSkipSentinel([])).toBe(false);
	});

	it('returns false for a non-empty list that is not the sentinel', () => {
		expect(isSkipSentinel(['tokyohot'])).toBe(false);
		expect(isSkipSentinel(['r18dev', 'dmm'])).toBe(false);
	});

	it('returns false for the sentinel plus other entries', () => {
		expect(isSkipSentinel([SKIP_SENTINEL, 'r18dev'])).toBe(false);
	});
});

describe('priority: buildFieldPriorityOverride (config shape)', () => {
	it('stores an exclusive override when it differs from global', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'] });
		const next = buildFieldPriorityOverride(config, 'series', ['tokyohot']);
		expect(next.series).toEqual(['tokyohot']);
	});

	it('preserves other field overrides when setting a new one', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { genre: ['dmm'] } });
		const next = buildFieldPriorityOverride(config, 'series', ['tokyohot']);
		expect(next.genre).toEqual(['dmm']);
		expect(next.series).toEqual(['tokyohot']);
	});

	it('deletes the key for a global-equal override (inherit = key absent, NOT [])', () => {
		// Global-equal ⇒ inherit. The config must encode inherit as an ABSENT key,
		// not []. So the key is deleted.
		const config = makeConfig({ global: ['r18dev', 'dmm'] });
		const next = buildFieldPriorityOverride(config, 'series', ['r18dev', 'dmm']);
		expect('series' in next).toBe(false);
		expect(next.series).toBeUndefined();
	});

	it('stores ["__skip__"] for a deliberate-empty override (Remove all + Save)', () => {
		// World A: "Remove all" + Save stores the skip sentinel, NOT [] (because
		// [] now means inherit). The sentinel is the only suppression encoding.
		const config = makeConfig({ global: ['r18dev', 'dmm'] });
		const next = buildFieldPriorityOverride(config, 'series', []);
		expect(next.series).toEqual([SKIP_SENTINEL]);
		expect('series' in next).toBe(true);
	});

	it('stores ["__skip__"] even when global is empty (Remove all still suppresses)', () => {
		// When the global list is itself empty, an empty editingPriority would
		// deep-equal global — but "Remove all" is a deliberate suppress action.
		// Persist the skip sentinel so the field stays suppressed even if global
		// scrapers are added later. The empty-list check runs before the
		// deep-equal shortcut for exactly this reason.
		const config = makeConfig({ global: [] });
		const next = buildFieldPriorityOverride(config, 'series', []);
		expect(next.series).toEqual([SKIP_SENTINEL]);
		expect('series' in next).toBe(true);
	});

	it('does not mutate the original config', () => {
		const config = makeConfig({ global: ['r18dev', 'dmm'], fields: { genre: ['dmm'] } });
		const snapshot = JSON.parse(JSON.stringify(config.metadata.priority));
		buildFieldPriorityOverride(config, 'series', ['tokyohot']);
		expect(JSON.parse(JSON.stringify(config.metadata.priority))).toEqual(snapshot);
	});
});

describe('priority: applyEnabledReorderToFull (disabled-scraper preservation)', () => {
	it('appends disabled scrapers after the reordered enabled ones (none dropped)', () => {
		// full stored list has 'javbus' disabled (hidden by the DraggableList)
		const full = ['r18dev', 'dmm', 'javbus'];
		// DraggableList shows only enabled; user reorders [r18dev, dmm] -> [dmm, r18dev]
		const newEnabledOrder = ['dmm', 'r18dev'];
		expect(applyEnabledReorderToFull(full, newEnabledOrder)).toEqual(['dmm', 'r18dev', 'javbus']);
	});

	it('preserves the relative order of multiple disabled scrapers', () => {
		const full = ['r18dev', 'dmm', 'javbus', 'javdb'];
		// enabled = [r18dev, dmm]; disabled = [javbus, javdb] (kept in original order)
		const newEnabledOrder = ['dmm', 'r18dev'];
		expect(applyEnabledReorderToFull(full, newEnabledOrder)).toEqual([
			'dmm',
			'r18dev',
			'javbus',
			'javdb',
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
		expect(applyEnabledReorderToFull(full, newEnabledOrder)).toEqual(['dmm', 'r18dev', 'javbus']);
	});
});

describe('priority: editor data flow preserves disabled scrapers through reorder + save', () => {
	// Mirrors MetadataPriority.svelte's component-local filterEnabledScrapers:
	// the DraggableList shows only scrapers whose config entry isn't disabled.
	function enabledView(config: SettingsConfig, priority: string[]): string[] {
		return priority.filter(
			(name) => (config.scrapers?.[name] as ScraperSettings | undefined)?.enabled !== false,
		);
	}

	it('edit a custom field + reorder + save keeps disabled scrapers in the override', () => {
		// Global priority lists 3 scrapers; 'javbus' is disabled.
		const config = makeConfigWithScrapers({
			global: ['r18dev', 'dmm', 'javbus'],
			disabled: ['javbus'],
			fields: { series: ['tokyohot'] },
		});

		// 1) User opens the editor: editingPriority loads the field's stored list.
		let editingPriority = [...getFieldPriority(config, 'series')];
		// Simulate the user adding the global scrapers back (Add all) so the
		// disabled 'javbus' re-enters the editable list.
		editingPriority = [...editingPriority, ...getGlobalPriority(config)];
		expect(editingPriority).toContain('javbus');

		// 2) DraggableList shows only enabled scrapers; user reorders them.
		const view = enabledView(config, editingPriority);
		const newEnabledOrder = [...view].reverse();
		editingPriority = applyEnabledReorderToFull(editingPriority, newEnabledOrder);

		// 3) Save: buildFieldPriorityOverride persists editingPriority (differs
		//    from global, so it is NOT collapsed to []). The disabled 'javbus'
		//    MUST survive — this is the CodeRabbit Finding 2 regression.
		const saved = buildFieldPriorityOverride(config, 'series', editingPriority);
		expect(saved.series).toContain('javbus'); // disabled scraper preserved
	});

	it('edit an inherited field + save (unchanged) restores "inherited" (key deleted)', () => {
		const config = makeConfigWithScrapers({
			global: ['r18dev', 'dmm', 'javbus'],
			disabled: ['javbus'],
		});

		// openFieldEditor loads the full stored list (== global for inherited).
		const editingPriority = [...getFieldPriority(config, 'series')];

		// Save unchanged: editingPriority === global => buildFieldPriorityOverride
		// DELETES the key (inherit = key absent), even though 'javbus' is
		// disabled. A present [] would mean "empty field", so inherit is encoded
		// as an absent key instead.
		const saved = buildFieldPriorityOverride(config, 'series', editingPriority);
		expect('series' in saved).toBe(false);
		expect(saved.series).toBeUndefined();
	});

	it('reorder an inherited field + save keeps disabled scrapers in the override', () => {
		// An inherited field (no override) edited + reordered + saved.
		const config = makeConfigWithScrapers({
			global: ['r18dev', 'dmm', 'javbus'],
			disabled: ['javbus'],
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
