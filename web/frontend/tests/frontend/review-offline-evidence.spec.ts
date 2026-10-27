/** Synthetic read-only UI contract; genuine source authority is tested separately. */
import { expect, test, type BrowserContext } from '@playwright/test';
import type { BatchJobResponse, OfflineEvidenceReport } from '../../src/lib/api/types';
import { getRescrapeModal, mockConfig } from './review-helpers';
const jobId = 'offline-evidence-browser',
	resultId = 'offline-evidence-result',
	movieId = 'EVIDENCE-001';
const file = `/synthetic/${movieId}.mp4`,
	resultPath = `/api/v1/batch/${jobId}/results/${resultId}`,
	timestamp = '2026-10-26T10:00:00Z';
const report: OfflineEvidenceReport = {
	outcome: 'evidence_available',
	admitted_at: timestamp,
	snapshot_digest: 'generation-A',
	observations: [
		{
			source: 'r18dev',
			source_id: '007',
			outcome: 'evidence_available',
			japanese_name: 'Source-only name',
			aliases: ['Whole source alias'],
			source_url: 'https://evidence.invalid/not-fetched',
			observed_at: timestamp,
		},
		{ source: 'r18dev', source_id: '7', outcome: 'unmatched' },
	],
};
async function mockAPI(context: BrowserContext, initialReport?: OfflineEvidenceReport) {
	const job: BatchJobResponse = {
		id: jobId,
		status: 'completed',
		total_files: 1,
		completed: 1,
		failed: 0,
		operation_count: 0,
		reverted_count: 0,
		noop_count: 0,
		excluded: {},
		progress: 100,
		destination: '/synthetic/output',
		started_at: timestamp,
		completed_at: timestamp,
		update: false,
		results: {
			[file]: {
				result_id: resultId,
				file_path: file,
				movie_id: movieId,
				status: 'completed',
				revision: 1,
				movie: {
					id: movieId,
					title: 'Unchanged current movie',
					actresses: [{ id: 1, first_name: 'Canonical', last_name: 'Actor' }],
					credits: [],
				},
				started_at: timestamp,
				ended_at: timestamp,
				is_multi_part: false,
				part_number: 0,
				part_suffix: '',
				offline_evidence: initialReport,
			},
		},
	};
	const state = {
		requests: [] as Record<string, unknown>[],
		unexpected: [] as string[],
		external: [] as string[],
	};
	await context.addInitScript(() => localStorage.setItem('javinizer-locale', 'en'));
	await context.routeWebSocket('**/ws/progress', () => {});
	await context.route('https://evidence.invalid/**', (route) => {
		state.external.push(route.request().url());
		return route.abort();
	});
	await context.route('**/api/v1/**', async (route) => {
		const request = route.request(),
			path = new URL(request.url()).pathname,
			method = request.method();
		if (method === 'GET' && path === `/api/v1/batch/${jobId}`) return route.fulfill({ json: job });
		if (method === 'POST' && path === `${resultPath}/rescrape`) {
			const body = request.postDataJSON();
			state.requests.push(body);
			job.results[file].offline_evidence = body.show_offline_evidence
				? structuredClone(report)
				: undefined;
			job.results[file].revision!++;
			return route.fulfill({
				json: {
					movie: job.results[file].movie,
					revision: job.results[file].revision,
					offline_evidence: job.results[file].offline_evidence,
				},
			});
		}
		if (method === 'GET' && path === `${resultPath}/existing-nfo`)
			return route.fulfill({ json: { exists: false } });
		if (method === 'POST' && path === `/api/v1/batch/${jobId}/video-collisions`)
			return route.fulfill({
				json: { items: [], new: 0, replaced: 0, skipped: 0, unresolved: 0, same_file: 0 },
			});
		if (method === 'POST' && path === `${resultPath}/preview`)
			return route.fulfill({
				json: {
					folder_name: movieId,
					file_name: `${movieId}.mp4`,
					full_path: `/synthetic/output/${movieId}.mp4`,
				},
			});
		if (method === 'POST' && path === `${resultPath}/display-title-preview`)
			return route.fulfill({ json: { display_title: 'Unchanged current movie' } });
		const reads: Record<string, unknown> = {
			'/api/v1/auth/status': { initialized: true, authenticated: true, username: 'synthetic' },
			'/api/v1/config': { ...mockConfig, output: { ...mockConfig.output, first_name_order: true } },
			'/api/v1/scrapers': {
				scrapers: [{ name: 'r18dev', display_title: 'R18.dev', enabled: true }],
			},
			'/api/v1/version': { current: 'test', update_available: false, source: 'disabled' },
			'/api/v1/jobs': { jobs: [], total: 0 },
			'/api/v1/genres/favorites': { favorites: [] },
			'/api/v1/actresses/collisions': { collisions: [] },
		};
		if (method === 'GET' && Object.hasOwn(reads, path)) return route.fulfill({ json: reads[path] });
		state.unexpected.push(`${method} ${path}`);
		return route.fulfill({ status: 501, json: { error: 'Unexpected mocked API request' } });
	});
	return state;
}
for (const mobile of [false, true]) {
	test(`stored R18 evidence is read-only and retains exact references (${mobile ? 'mobile' : 'desktop'})`, async ({
		page,
		context,
	}) => {
		if (mobile) await page.setViewportSize({ width: 390, height: 844 });
		const state = await mockAPI(context, report);
		await page.goto(`/review/${jobId}?view=detail`);
		const panel = page.getByRole('region', { name: 'R18 source evidence', exact: true });
		await expect(panel).toBeVisible();
		await expect(panel.getByText('r18dev:007', { exact: true })).toBeVisible();
		await expect(panel.getByText('r18dev:7', { exact: true })).toBeVisible();
		await expect(panel.getByText('Source-only name', { exact: true })).toBeVisible();
		await expect(page.getByText('Canonical Actor', { exact: true })).toBeVisible();
		await expect(panel.locator('button,input,img,a')).toHaveCount(0);
		await panel.getByText('Stored snapshot details', { exact: true }).click();
		await expect(panel.getByText('generation-A', { exact: true })).toBeVisible();
		await page.reload();
		await expect(panel).toBeVisible();
		expect(state.requests).toEqual([]);
		expect(state.external).toEqual([]);
		expect(state.unexpected).toEqual([]);
	});
}
test('rescrape requires a fresh opt-in and a later opt-out clears the stored report', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	await page.goto(`/review/${jobId}?view=detail`);
	const panel = page.getByRole('region', { name: 'R18 source evidence', exact: true });
	await expect(page.getByText('Canonical Actor', { exact: true })).toBeVisible();
	await expect(panel).toHaveCount(0);
	await page.getByRole('button', { name: 'Rescrape', exact: true }).click();
	const modal = getRescrapeModal(page),
		option = modal.getByRole('checkbox', { name: /Show offline R18/ });
	await expect(option).not.toBeChecked();
	await option.check();
	await modal.getByRole('button', { name: 'Rescrape', exact: true }).click();
	await expect(modal).not.toBeVisible();
	await expect(panel).toBeVisible();
	expect(state.requests[0].show_offline_evidence).toBe(true);
	await page.getByRole('button', { name: 'Rescrape', exact: true }).click();
	await expect(option).not.toBeChecked();
	await modal.getByRole('button', { name: 'Rescrape', exact: true }).click();
	await expect(modal).not.toBeVisible();
	await expect(panel).toHaveCount(0);
	expect(state.requests[1]).not.toHaveProperty('show_offline_evidence');
	await expect(page.getByText('Canonical Actor', { exact: true })).toBeVisible();
	expect(state.external).toEqual([]);
	expect(state.unexpected).toEqual([]);
});
