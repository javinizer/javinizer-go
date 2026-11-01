/** Synthetic UI contract: identity resolution and persistence are tested in Go. */
import { expect, test } from '@playwright/test';
import type { BatchJobResponse, CreditCollision } from '../../src/lib/api/types';
import { mockConfig } from './review-helpers';
import { createRequire } from 'node:module';
const { installOfflineEvidenceBrowserGuard } = createRequire(import.meta.url)(
	'../fullstack/helpers/offline-evidence-browser.cjs',
);

for (const width of [1280, 390]) {
	test(`same-name conflicts expose identities and safe relinking at ${width}px`, async ({
		page,
		context,
	}) => {
		await page.setViewportSize({ width, height: 900 });
		await installOfflineEvidenceBrowserGuard(context);
		const external: string[] = [];
		context.on('request', (request) => {
			if (!['localhost', '127.0.0.1', '[::1]'].includes(new URL(request.url()).hostname))
				external.push(request.url());
		});
		await context.addInitScript(() => localStorage.setItem('javinizer-locale', 'en'));
		await context.routeWebSocket('**/ws/progress', () => {});
		const identity = {
			id: 7,
			dmm_id: 0,
			first_name: 'Nagisa',
			last_name: 'Hazuki',
			japanese_name: '',
			verified: false,
		};
		const targets = [
			{ ...identity, id: 8, dmm_id: 1109243, japanese_name: '葉月なぎさ', verified: true },
			{ ...identity, id: 9, dmm_id: 1034095, japanese_name: '葉月渚', verified: true },
		];
		const longPrefix = 'https://images.invalid/' + 'shared-path/'.repeat(25);
		let collisions: CreditCollision[] = [
			{
				id: 1,
				credit_id: 1,
				movie_content_id: 'identities',
				field: 'identity_link',
				reported_value: 'Hazuki Nagisa',
				canonical_value: 'Hazuki Nagisa',
				status: 'open',
				occurrences: 1,
				current_actress_id: 7,
				current_actress: identity,
				allowed_resolutions: ['adopt_canonical', 'reassign'],
			},
			{
				id: 2,
				credit_id: 1,
				movie_content_id: 'identities',
				field: 'reported_thumb_url',
				reported_value: longPrefix + 'reported.jpg',
				canonical_value: longPrefix + 'catalog.jpg',
				status: 'open',
				occurrences: 1,
				current_actress_id: 7,
				current_actress: identity,
				allowed_resolutions: ['reassign'],
			},
		];
		const file = '/synthetic/IDENTITY-1.mp4';
		const job: BatchJobResponse = {
			id: 'collision-details',
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
			update: false,
			started_at: '2026-11-01T00:00:00Z',
			results: {
				[file]: {
					result_id: 'identity-result',
					file_path: file,
					movie_id: 'IDENTITY-1',
					status: 'completed',
					started_at: '2026-11-01T00:00:00Z',
					is_multi_part: false,
					part_number: 0,
					part_suffix: '',
					movie: {
						id: 'IDENTITY-1',
						code: 'identities',
						title: 'Identity review fixture',
						actresses: [identity],
						credits: [],
					},
				},
			},
		};
		const collisionMovieIDs: string[] = [];
		const mutations: unknown[] = [];
		const unexpected: string[] = [];
		await context.route('**/api/v1/**', async (route) => {
			const request = route.request(),
				path = new URL(request.url()).pathname;
			if (request.method() === 'GET' && path === '/api/v1/actresses/collisions') {
				collisionMovieIDs.push(new URL(request.url()).searchParams.get('movie_id') ?? '');
			}
			if (request.method() === 'POST' && path === '/api/v1/actresses/collisions/1/resolve') {
				mutations.push(request.postDataJSON());
				collisions = collisions.filter((row) => row.id !== 1);
				return route.fulfill({ json: { resolved: true, remaining_open: 1 } });
			}
			if (request.method() === 'POST' && path.endsWith('/video-collisions'))
				return route.fulfill({
					json: { items: [], new: 0, replaced: 0, skipped: 0, unresolved: 0, same_file: 0 },
				});
			if (request.method() === 'POST' && path.endsWith('/display-title-preview'))
				return route.fulfill({ json: { display_title: 'Identity review fixture' } });
			if (request.method() === 'POST' && path.endsWith('/preview'))
				return route.fulfill({
					json: {
						folder_name: 'IDENTITY-1',
						file_name: 'IDENTITY-1.mp4',
						full_path: '/synthetic/output/IDENTITY-1.mp4',
					},
				});
			const reads: Record<string, unknown> = {
				'/api/v1/auth/status': { initialized: true, authenticated: true, username: 'fixture' },
				'/api/v1/config': mockConfig,
				'/api/v1/scrapers': { scrapers: [] },
				'/api/v1/version': { current: 'test', update_available: false, source: 'disabled' },
				'/api/v1/jobs': { jobs: [], total: 0 },
				'/api/v1/genres/favorites': { favorites: [] },
				'/api/v1/batch/collision-details': job,
				'/api/v1/batch/collision-details/results/identity-result/existing-nfo': { exists: false },
				'/api/v1/actresses/collisions': { collisions },
				'/api/v1/actresses/search': targets,
			};
			if (request.method() === 'GET' && Object.hasOwn(reads, path))
				return route.fulfill({ json: reads[path] });
			unexpected.push(`${request.method()} ${path}`);
			return route.fulfill({ status: 501, json: { error: 'Unexpected mocked API request' } });
		});
		await page.goto('/review/collision-details?view=detail');
		// The public MovieView uses code, not the persistence-layer content_id.
		// Prove this fixture reaches the real panel before checking its content.
		await expect.poll(() => collisionMovieIDs).toContain('identities');
		const explanation = page.getByText(/Matching names do not confirm/);
		await expect(explanation).toBeVisible();
		await expect(page.getByText('Unverified candidate', { exact: true })).toHaveCount(2);
		const reported = page.getByText(longPrefix + 'reported.jpg', { exact: true });
		await expect(reported).toBeVisible();
		expect(await reported.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);
		expect(await reported.evaluate((el) => getComputedStyle(el).textOverflow)).not.toBe('ellipsis');
		await page.screenshot({
			path: test.info().outputPath(`collision-details-${width}-unresolved.png`),
			fullPage: true,
		});
		await page.getByRole('button', { name: 'Relink', exact: true }).first().click();
		let dialog = page.getByRole('dialog', { name: 'Choose verified actress to relink' });
		await expect(dialog.getByRole('button', { name: /#8.*葉月なぎさ.*1109243/ })).toBeVisible();
		await expect(dialog.getByRole('button', { name: /#9.*葉月渚.*1034095/ })).toBeVisible();
		await dialog.getByRole('button', { name: /#8.*葉月なぎさ.*1109243/ }).click();
		await expect(dialog.getByText('Selected target: Hazuki Nagisa #8')).toBeVisible();
		await expect(dialog.getByText('葉月なぎさ', { exact: true })).toHaveCount(2);
		await page.screenshot({
			path: test.info().outputPath(`collision-details-${width}-selected.png`),
			fullPage: true,
		});
		await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
		await expect(dialog).toHaveCount(0);
		expect(mutations).toEqual([]);
		await page.getByRole('button', { name: 'Relink', exact: true }).first().click();
		dialog = page.getByRole('dialog', { name: 'Choose verified actress to relink' });
		await expect(dialog.getByRole('button', { name: 'Confirm relink' })).toBeDisabled();
		await dialog.getByRole('button', { name: /#9.*葉月渚.*1034095/ }).click();
		await dialog.getByRole('button', { name: 'Confirm relink' }).click();
		await expect(explanation).toHaveCount(0);
		expect(mutations).toEqual([{ resolution: 'reassign', target_actress_id: 9 }]);
		expect(unexpected).toEqual([]);
		expect(external).toEqual([]);
		await page.screenshot({
			path: test.info().outputPath(`collision-details-${width}.png`),
			fullPage: true,
		});
	});
}
