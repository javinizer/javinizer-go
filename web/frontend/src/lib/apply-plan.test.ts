import { describe, expect, it } from 'vitest';
import {
	applyPlanSummary,
	applyPreset,
	compactApplyPlanSummary,
	defaultApplyPlan,
	initialApplyPlan,
	migrateLegacyPlan,
	normalizeApplyPlan,
	projectLegacyPlan,
	scalarStrategyLabel,
	setMergeStrategies,
	validateApplyPlan,
} from './apply-plan';

describe('apply plan', () => {
	it.each([
		['organize', 'organize', false],
		['rename-in-place', 'in-place', false],
		['rename-file', 'in-place-norenamefolder', false],
		['leave-in-place', 'metadata-artwork', true],
		['metadata-artwork', 'metadata-artwork', false],
	] as const)('projects %s', (operation, mode, update) => {
		const plan = defaultApplyPlan(operation, operation === 'organize' ? '/dest' : '/stale');
		expect(projectLegacyPlan(plan)).toMatchObject({ operation_mode: mode, update });
		expect(normalizeApplyPlan(plan).destination).toBe(
			operation === 'organize' ? '/dest' : undefined,
		);
	});

	it('rejects unsupported runtime versions, enums, and replacement combinations', () => {
		const invalidVersion = { ...defaultApplyPlan('organize', '/dest'), version: 2 } as never;
		expect(() => normalizeApplyPlan(invalidVersion)).toThrow(/version/);
		const invalidEnum = {
			...defaultApplyPlan('organize', '/dest'),
			media_policy: 'unknown',
		} as never;
		expect(() => normalizeApplyPlan(invalidEnum)).toThrow(/media policy/);
		const replacement = defaultApplyPlan('rename-file');
		replacement.media_policy = 'replace';
		expect(validateApplyPlan(replacement)).toContain(
			'Existing media can only be replaced when video files stay in place.',
		);
	});

	it('preserves metadata-artwork while rejecting preview', () => {
		expect(initialApplyPlan('metadata-artwork')?.video_operation).toBe('metadata-artwork');
		expect(initialApplyPlan('preview')).toBeNull();
	});

	it('maps presets and clears provenance after custom edits', () => {
		const preset = applyPreset(defaultApplyPlan('leave-in-place'), 'aggressive');
		expect(preset.merge).toEqual({
			scalar_strategy: 'prefer-scraper',
			array_strategy: 'replace',
			source_preset: 'aggressive',
		});
		expect(setMergeStrategies(preset, 'prefer-nfo', 'merge').merge?.source_preset).toBeUndefined();

		// Round-6 review pin: summaries render LOCALIZED strategy labels, never
		// raw enum identifiers (regression: "prefer-scraper" leaked into the UI).
		const summary = applyPlanSummary(preset);
		const mergeLine = summary.find((l: string) => l.startsWith('Existing metadata:'));
		expect(mergeLine).toContain('Prefer Scraped');
		expect(mergeLine).toContain('Replace');
		expect(mergeLine).not.toContain('prefer-scraper');
		// Unknown forward-compat values pass through identifiably.
		expect(scalarStrategyLabel('future-strategy' as never)).toBe('future-strategy');
	});

	it('rejects no-op metadata-artwork and update plans', () => {
		const update = defaultApplyPlan('leave-in-place');
		update.nfo_output = 'skip';
		update.media_policy = 'skip';
		expect(validateApplyPlan(update)).toHaveLength(1);
		const metadata = defaultApplyPlan('metadata-artwork');
		metadata.nfo_output = 'skip';
		metadata.media_policy = 'skip';
		expect(validateApplyPlan(metadata)).toHaveLength(1);
		const rename = defaultApplyPlan('rename-file');
		rename.nfo_output = 'skip';
		rename.media_policy = 'skip';
		expect(validateApplyPlan(rename)).toEqual([]);
	});

	it('migrates update snapshots regardless of stale mode and rejects contradictory state', () => {
		expect(
			migrateLegacyPlan({ browseMode: 'update', update: true, effectiveOperationMode: 'in-place' })
				.plan?.video_operation,
		).toBe('leave-in-place');
		expect(
			migrateLegacyPlan({
				browseMode: 'update',
				update: true,
				effectiveOperationMode: 'metadata-artwork',
			}).plan?.video_operation,
		).toBe('leave-in-place');
		expect(
			migrateLegacyPlan({ browseMode: 'update', update: true, effectiveOperationMode: 'organize' })
				.plan?.video_operation,
		).toBe('leave-in-place');
		expect(
			migrateLegacyPlan({ browseMode: 'scrape', update: false, effectiveOperationMode: 'preview' }),
		).toMatchObject({ plan: null });
		expect(
			migrateLegacyPlan({ browseMode: 'scrape', update: true, effectiveOperationMode: 'organize' }),
		).toMatchObject({ plan: null });
	});

	it('derives detailed and compact summaries from the same projection', () => {
		const plan = defaultApplyPlan('organize', '/library');
		expect(compactApplyPlanSummary(plan)).toBe(applyPlanSummary(plan).join(' · '));
	});
});
