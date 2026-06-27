import { describe, it, expect } from 'vitest';
import {
	SKIP_FIELD_SENTINEL,
	getGlobalPriority,
	getFieldPriority,
	isSkipField,
	isFieldOverridden,
	getFieldStatus,
	buildFieldPriorityOverride
} from './priority';
import type { SettingsConfig } from '$lib/api/types';

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
