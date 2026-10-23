/**
 * Browser regressions with a fully mocked API. These pin browser sessionStorage,
 * reloads, tab isolation, and the actual controls/request payloads; they do not
 * establish backend admission correctness. The companion fullstack spec does.
 * Included by the existing playwright.frontend.config.ts / frontend-only CI job.
 */
import { test, expect, type BrowserContext, type Page } from '@playwright/test';
import type { BatchJobResponse, OrganizeRequest, UpdateRequest } from '../../src/lib/api/types';
import { mockConfig } from './review-helpers';

const JOB_ID = 'receipt-browser-job';
const SOURCE = '/synthetic/RECEIPT-001.mp4';
const DESTINATION = '/synthetic/original-output';
const KEY = `javinizer.review.applyRecovery.${JOB_ID}`;
const UNKNOWN = 'The original apply attempt or settings could not be verified';
const timestamp = '2026-01-01T00:00:00Z';

type Reply = 'accepted' | 'rejected' | 'missing' | 'zero' | 'string' | 'unsafe' | 'lost';

function job(generation = 0, update = false): BatchJobResponse {
	return {
		id: JOB_ID,
		status: 'completed', // The real worker keeps partial-failure envelopes completed.
		total_files: 1,
		completed: generation === 0 ? 1 : 0,
		failed: generation === 0 ? 0 : 1,
		operation_count: 0,
		reverted_count: 0,
		noop_count: 0,
		excluded: {},
		progress: 100,
		destination: '/synthetic/default-output',
		apply_generation: generation,
		started_at: timestamp,
		completed_at: timestamp,
		update,
		...(update
			? {
					apply_plan: {
						version: 1 as const,
						video_operation: 'leave-in-place' as const,
						destination: '',
						nfo_output: 'write' as const,
						media_policy: 'missing' as const,
					},
				}
			: {}),
		results: {
			[SOURCE]: {
				result_id: 'receipt-result',
				file_path: SOURCE,
				movie_id: 'RECEIPT-001',
				status: generation === 0 ? 'completed' : 'failed',
				...(generation > 0
					? { error: 'Synthetic credit conflict', error_code: 'credit_conflict' as const }
					: {}),
				revision: 1,
				movie: {
					id: 'RECEIPT-001',
					title: 'Synthetic receipt fixture',
					actresses: [],
					credits: [],
				},
				started_at: timestamp,
				ended_at: timestamp,
				is_multi_part: false,
				part_number: 0,
				part_suffix: '',
			},
		},
	};
}

async function mockAPI(context: BrowserContext, update = false) {
	const state = {
		generation: 0,
		reply: 'accepted' as Reply,
		posts: [] as (OrganizeRequest | UpdateRequest)[],
		videoReviews: [] as OrganizeRequest[],
		videoReviewError: false,
		unexpected: [] as string[],
	};
	// Keep the frontend-only suite offline, including its otherwise reconnecting WS.
	await context.routeWebSocket('**/ws/progress', () => {});
	await context.route('**/api/v1/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const method = request.method();
		if (method === 'POST' && path === `/api/v1/batch/${JOB_ID}/video-collisions`) {
			state.videoReviews.push(request.postDataJSON() as OrganizeRequest);
			if (state.videoReviewError)
				return route.fulfill({
					status: 503,
					json: { error: 'Synthetic video review unavailable' },
				});
			return route.fulfill({
				json: { items: [], new: 1, replaced: 0, skipped: 0, unresolved: 0, same_file: 0 },
			});
		}
		if (method === 'POST' && path === `/api/v1/batch/${JOB_ID}/${update ? 'update' : 'organize'}`) {
			state.posts.push(request.postDataJSON() as OrganizeRequest | UpdateRequest);
			// On rejected/unknown responses, this newer GET could belong to another tab.
			// Observing N+1 must never replace the missing request-specific receipt.
			state.generation += 1;
			if (state.reply === 'lost') return route.abort('failed');
			if (state.reply === 'rejected')
				return route.fulfill({
					status: 409,
					json: { error: 'Synthetic admission rejected', code: 'APPLY_GENERATION_MISMATCH' },
				});
			const receipt = {
				accepted: state.generation,
				missing: undefined,
				zero: 0,
				string: String(state.generation),
				unsafe: Number.MAX_SAFE_INTEGER + 1,
			}[state.reply];
			return route.fulfill({
				json: {
					message: update ? 'Update started' : 'Organization started',
					apply_generation: receipt,
				},
			});
		}
		if (method === 'GET' && path === `/api/v1/batch/${JOB_ID}`)
			return route.fulfill({ json: job(state.generation, update) });
		if (method === 'GET' && path === '/api/v1/auth/status')
			return route.fulfill({
				json: { initialized: true, authenticated: true, username: 'synthetic' },
			});
		if (method === 'GET' && path === '/api/v1/config') return route.fulfill({ json: mockConfig });
		if (method === 'GET' && path === '/api/v1/scrapers')
			return route.fulfill({ json: { scrapers: [] } });
		if (method === 'GET' && path === '/api/v1/version')
			return route.fulfill({
				json: { current: 'test', update_available: false, source: 'disabled' },
			});
		if (method === 'GET' && path === '/api/v1/jobs')
			return route.fulfill({ json: { jobs: [], total: 0 } });
		if (method === 'GET' && path === '/api/v1/genres/favorites')
			return route.fulfill({ json: { favorites: [] } });
		if (method === 'GET' && path === '/api/v1/actresses/collisions')
			return route.fulfill({ json: { collisions: [] } });
		if (method === 'GET' && path === `/api/v1/batch/${JOB_ID}/results/receipt-result/existing-nfo`)
			return route.fulfill({ json: { exists: false } });
		if (method === 'POST' && path === `/api/v1/batch/${JOB_ID}/results/receipt-result/preview`)
			return route.fulfill({
				json: {
					folder_name: 'RECEIPT-001',
					file_name: 'RECEIPT-001.mp4',
					full_path: `${DESTINATION}/RECEIPT-001.mp4`,
				},
			});
		if (
			method === 'POST' &&
			path === `/api/v1/batch/${JOB_ID}/results/receipt-result/display-title-preview`
		)
			return route.fulfill({ json: { display_title: 'Synthetic receipt fixture' } });
		state.unexpected.push(`${method} ${path}`);
		return route.fulfill({ status: 501, json: { error: 'Unexpected mocked API request' } });
	});
	return state;
}

const retry = (page: Page) => page.getByRole('button', { name: 'Retry Failed', exact: true });
const apply = (page: Page) => page.getByRole('button', { name: /^Organize 1 File$/ }).first();
const recovery = (page: Page) => page.evaluate((key) => sessionStorage.getItem(key), KEY);

async function configure(page: Page, operation: string) {
	await page.getByLabel('File operation', { exact: true }).selectOption(operation);
	await page.getByPlaceholder('Enter destination path (e.g., /path/to/output)').fill(DESTINATION);
	await page.getByRole('checkbox', { name: /Skip NFO Generation/ }).check();
	await page.getByRole('checkbox', { name: /Skip Media Download/ }).check();
}

test('failed video review blocks Apply without creating a receipt until an explicit successful refresh', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	state.videoReviewError = true;
	await page.goto(`/review/${JOB_ID}?view=detail`);
	await configure(page, 'move');
	await expect(
		page.getByRole('alert').filter({ hasText: 'Synthetic video review unavailable' }),
	).toBeVisible();
	await expect(apply(page)).toBeDisabled();
	expect(state.posts).toEqual([]);
	expect(await recovery(page)).toBeNull();
	state.videoReviewError = false;
	await page.getByRole('button', { name: 'Review / refresh destinations', exact: true }).click();
	await expect(apply(page)).toBeEnabled();
	await apply(page).click();
	await expect(retry(page)).toBeEnabled();
	expect(state.posts).toHaveLength(1);
	expect(JSON.parse((await recovery(page))!)).toMatchObject({ acceptedApplyGeneration: 1 });
	expect(state.unexpected).toEqual([]);
});

for (const operation of ['move', 'copy', 'hardlink', 'softlink']) {
	test(`accepted ${operation} receipt preserves original controls through retry, reload, and another retry`, async ({
		page,
		context,
	}) => {
		const state = await mockAPI(context);
		await page.goto(`/review/${JOB_ID}?view=detail`);
		await configure(page, operation);
		await apply(page).click();
		await expect(retry(page)).toBeEnabled();
		expect(state.posts).toHaveLength(1);
		const original = structuredClone(state.posts[0]) as OrganizeRequest;
		expect(original).toMatchObject({
			destination: DESTINATION,
			operation_mode: 'organize',
			copy_only: operation !== 'move',
			skip_nfo: true,
			skip_download: true,
		});
		expect(original.link_mode).toBe(
			operation === 'hardlink' ? 'hard' : operation === 'softlink' ? 'soft' : undefined,
		);
		expect(original).not.toHaveProperty('expected_apply_generation');
		expect(state.videoReviews.length).toBeGreaterThan(0);
		expect(state.videoReviews.at(-1)).toMatchObject({
			destination: DESTINATION,
			operation_mode: 'organize',
			copy_only: operation !== 'move',
		});
		expect(JSON.parse((await recovery(page))!)).toMatchObject({
			acceptedApplyGeneration: 1,
			organizeOperation: operation,
			destination: DESTINATION,
			skipNfo: true,
			skipDownload: true,
		});

		for (const generation of [1, 2]) {
			await page.reload();
			await expect(retry(page)).toBeEnabled();
			await expect(page.getByLabel('File operation', { exact: true })).toHaveValue(operation);
			await page
				.getByLabel('File operation', { exact: true })
				.selectOption(operation === 'move' ? 'copy' : 'move');
			await page
				.getByPlaceholder('Enter destination path (e.g., /path/to/output)')
				.fill('/synthetic/changed-output');
			await page.getByRole('checkbox', { name: /Skip NFO Generation/ }).uncheck();
			await page.getByRole('checkbox', { name: /Skip Media Download/ }).uncheck();
			await retry(page).click();
			await expect.poll(() => state.posts.length).toBe(generation + 1);
			// Exact equality also pins the original immutable overrides, not just top-level flags.
			expect(state.posts[generation]).toEqual({
				...original,
				retry_file_paths: [SOURCE],
				expected_apply_generation: generation,
			});
			await expect(retry(page)).toBeEnabled();
			expect(JSON.parse((await recovery(page))!)).toMatchObject({
				acceptedApplyGeneration: generation + 1,
				organizeOperation: operation,
				destination: DESTINATION,
				skipNfo: true,
				skipDownload: true,
			});
		}
		expect(state.unexpected).toEqual([]);
	});
}

for (const reply of ['rejected', 'missing', 'zero', 'string', 'unsafe', 'lost'] as const) {
	test(`${reply} response cannot authorize Retry Failed from a newer GET or reload`, async ({
		page,
		context,
	}) => {
		const state = await mockAPI(context);
		state.reply = reply;
		await page.goto(`/review/${JOB_ID}?view=detail`);
		await configure(page, 'hardlink');
		await apply(page).click();
		await expect.poll(() => state.posts.length).toBe(1);
		await expect(apply(page)).toBeEnabled(); // Rejection/receipt validation has settled.
		expect(await recovery(page)).toBeNull();
		await expect(retry(page).and(page.locator(':enabled'))).toHaveCount(0);
		await page.reload();
		await expect(page.getByRole('status').filter({ hasText: UNKNOWN })).toBeVisible();
		await expect(retry(page)).toBeDisabled();
		expect(await recovery(page)).toBeNull();
		expect(state.posts).toHaveLength(1);
		expect(state.unexpected).toEqual([]);
	});
}

test('same-origin tabs cannot adopt or erase another tab’s recovery or legacy localStorage', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	await page.goto(`/review/${JOB_ID}?view=detail`);
	await configure(page, 'hardlink');
	await apply(page).click();
	await expect(retry(page)).toBeEnabled();
	const firstRecord = await recovery(page);
	expect(firstRecord).not.toBeNull();
	// Deliberately seed a valid legacy shared record: the new tab must ignore it.
	await page.evaluate(({ key, record }) => localStorage.setItem(key, record!), {
		key: KEY,
		record: firstRecord,
	});
	const other = await context.newPage(); // Same localStorage, separate real sessionStorage.
	try {
		await other.goto(`/review/${JOB_ID}?view=detail`);
		await expect(retry(other)).toBeDisabled();
		expect(await recovery(other)).toBeNull();
		expect(await recovery(page)).toBe(firstRecord);
		await configure(other, 'copy');
		await apply(other).click();
		await expect(retry(other)).toBeEnabled();
		expect(JSON.parse((await recovery(other))!)).toMatchObject({
			acceptedApplyGeneration: 2,
			organizeOperation: 'copy',
		});
		expect(await recovery(page)).toBe(firstRecord);
		state.reply = 'rejected';
		await apply(other).click();
		await expect(apply(other)).toBeEnabled();
		expect(await recovery(other)).toBeNull(); // Only the rejecting tab loses authority.
		expect(await recovery(page)).toBe(firstRecord);
		await page.reload();
		await expect(retry(page)).toBeDisabled(); // Foreign generation cannot reuse the old receipt.
		expect(await recovery(page)).toBe(firstRecord); // Nor erase another valid generation's record.
		expect(await page.evaluate((key) => localStorage.getItem(key), KEY)).toBe(firstRecord);
		expect(state.posts).toHaveLength(3);
		expect(state.unexpected).toEqual([]);
	} finally {
		await other.close();
	}
});

for (const flag of ['Force Overwrite', 'Preserve NFO']) {
	test(`accepted Update receipt retains ${flag} and NFO options across reload`, async ({
		page,
		context,
	}) => {
		const state = await mockAPI(context, true);
		await page.goto(`/review/${JOB_ID}?view=detail`);
		await page.getByRole('button', { name: 'Options', exact: true }).click();
		await page.getByRole('checkbox', { name: new RegExp(`^${flag}`) }).check();
		await page.getByRole('checkbox', { name: /^Skip Download/ }).check();
		await page.getByRole('button', { name: 'Update 1 File', exact: true }).click();
		await expect(retry(page)).toBeEnabled();
		expect(state.posts).toHaveLength(1);
		const original = structuredClone(state.posts[0]) as UpdateRequest;
		expect(original.overrides).toMatchObject({
			force_overwrite: flag === 'Force Overwrite',
			preserve_nfo: flag === 'Preserve NFO',
			skip_nfo: false,
			skip_download: true,
		});
		expect(original).not.toHaveProperty('expected_apply_generation');
		for (const generation of [1, 2]) {
			await page.reload();
			await expect(retry(page)).toBeEnabled();
			await page.getByRole('button', { name: 'Options', exact: true }).click();
			await page.getByRole('checkbox', { name: /^Force Overwrite/ }).uncheck();
			await page.getByRole('checkbox', { name: /^Preserve NFO/ }).uncheck();
			await page.getByRole('checkbox', { name: /^Skip Download/ }).uncheck();
			await page.getByRole('checkbox', { name: /^Skip NFO/ }).check();
			await retry(page).click();
			await expect.poll(() => state.posts.length).toBe(generation + 1);
			expect(state.posts[generation]).toEqual({
				...original,
				retry_file_paths: [SOURCE],
				expected_apply_generation: generation,
			});
			await expect(retry(page)).toBeEnabled();
			expect(JSON.parse((await recovery(page))!)).toMatchObject({
				operation: 'update',
				acceptedApplyGeneration: generation + 1,
				updateOptions: original,
			});
		}
		expect(state.unexpected).toEqual([]);
	});
}

for (const reply of ['rejected', 'missing'] as const) {
	test(`${reply} Update receipt does not fall back to live settings after reload`, async ({
		page,
		context,
	}) => {
		const state = await mockAPI(context, true);
		state.reply = reply;
		await page.goto(`/review/${JOB_ID}?view=detail`);
		const update = page.getByRole('button', { name: 'Update 1 File', exact: true });
		await update.click();
		await expect.poll(() => state.posts.length).toBe(1);
		await expect(update).toBeEnabled();
		expect(await recovery(page)).toBeNull();
		await page.reload();
		await expect(page.getByRole('status').filter({ hasText: UNKNOWN })).toBeVisible();
		await expect(retry(page)).toBeDisabled();
		expect(await recovery(page)).toBeNull();
		expect(state.posts).toHaveLength(1);
		expect(state.unexpected).toEqual([]);
	});
}
