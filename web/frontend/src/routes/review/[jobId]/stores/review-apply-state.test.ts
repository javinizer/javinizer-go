import { describe, expect, it } from 'vitest';
import type { BatchJobResponse } from '$lib/api/types';
import {
	buildReviewApplyOverrides,
	hydrateReviewApplyControls,
	shouldHydrateReviewApplyControls,
	withCustomReviewMergeStrategy,
	shouldClearUnstartedApplyRecovery,
} from './review-state.svelte';

function makeJob(): BatchJobResponse {
	return {
		id: 'job-1',
		status: 'completed',
		total_files: 1,
		completed: 1,
		failed: 0,
		operation_count: 0,
		reverted_count: 0,
		noop_count: 0,
		excluded: {},
		progress: 100,
		destination: '/legacy',
		results: {},
		started_at: '2026-01-01T00:00:00Z',
		update: true,
		apply_plan: {
			version: 1,
			video_operation: 'leave-in-place',
			nfo_output: 'write',
			media_policy: 'replace',
			merge: {
				scalar_strategy: 'prefer-scraper',
				array_strategy: 'replace',
				source_preset: 'aggressive',
			},
		},
	};
}

describe('Review apply state', () => {
	it('hydrates once and ignores polling data for the already hydrated job', () => {
		const job = makeJob();
		expect(shouldHydrateReviewApplyControls(null, job, 'job-1')).toBe(true);
		const controls = hydrateReviewApplyControls(job);
		controls.skipNfo = true;
		expect(shouldHydrateReviewApplyControls('job-1', { ...job, progress: 75 }, 'job-1')).toBe(
			false,
		);
		expect(controls.skipNfo).toBe(true);
	});

	it('refresh restores the original proposed plan', () => {
		const job = makeJob();
		const edited = hydrateReviewApplyControls(job);
		edited.overwriteExistingMedia = false;
		edited.applyScalarStrategy = 'prefer-nfo';
		const refreshed = hydrateReviewApplyControls(job);
		expect(refreshed.overwriteExistingMedia).toBe(true);
		expect(refreshed.applyScalarStrategy).toBe('prefer-scraper');
		expect(refreshed.applyPreset).toBe('aggressive');
	});

	it('clears preset provenance after a custom strategy edit', () => {
		const controls = hydrateReviewApplyControls(makeJob());
		const edited = withCustomReviewMergeStrategy(controls, { scalar: 'prefer-nfo' });
		expect(edited.applyPreset).toBeUndefined();
		expect(edited.applyScalarStrategy).toBe('prefer-nfo');
		expect(edited.applyArrayStrategy).toBe('replace');
	});

	it('builds a faithful update payload including replacement and advanced override', () => {
		const controls = hydrateReviewApplyControls(makeJob());
		controls.forceOverwrite = true;
		const overrides = buildReviewApplyOverrides(controls, true, 'metadata-artwork');
		expect(overrides).toEqual({
			operation_mode: 'metadata-artwork',
			destination: '',
			skip_nfo: false,
			skip_download: false,
			overwrite_existing_media: true,
			preset: 'aggressive',
			scalar_strategy: 'prefer-scraper',
			array_strategy: 'replace',
			force_overwrite: true,
			preserve_nfo: false,
		});
	});

	it('represents preserve-NFO separately from canonical strategies', () => {
		const controls = hydrateReviewApplyControls(makeJob());
		controls.preserveNfo = true;
		const overrides = buildReviewApplyOverrides(controls, true, 'metadata-artwork');
		expect(overrides.preserve_nfo).toBe(true);
		expect(overrides.force_overwrite).toBe(false);
		expect(overrides.scalar_strategy).toBe('prefer-scraper');
	});

	it('maps organize controls without unsupported update-only fields', () => {
		const controls = {
			...hydrateReviewApplyControls(makeJob()),
			destinationPath: '/output',
			skipDownload: true,
			overwriteExistingMedia: false,
		};
		const overrides = buildReviewApplyOverrides(controls, false, 'organize');
		expect(overrides).toEqual({
			operation_mode: 'organize',
			destination: '/output',
			skip_nfo: false,
			skip_download: true,
			overwrite_existing_media: false,
		});
		expect(overrides).not.toHaveProperty('scalar_strategy');
		expect(overrides).not.toHaveProperty('force_overwrite');
	});

	it('rejects stale placeholder hydration for another route job', () => {
		expect(shouldHydrateReviewApplyControls(null, makeJob(), 'job-2')).toBe(false);
	});

	it('preserves equal-generation recovery while a local apply is pending', () => {
		const job = { ...makeJob(), failed: 1, apply_generation: 4 };
		const recovery = { preApplyGeneration: 4, failed: {}, succeeded: [] };

		expect(shouldClearUnstartedApplyRecovery(job, recovery, true)).toBe(false);
		expect(shouldClearUnstartedApplyRecovery(job, recovery, false)).toBe(true);
	});
});
