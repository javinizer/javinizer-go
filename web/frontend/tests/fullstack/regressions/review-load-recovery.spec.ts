/**
 * Review load recovery regressions.
 *
 * These tests use the real full-stack backend for setup and successful paths.
 * The only page.route exception is deterministic failure/hold injection for
 * GET /api/v1/batch/:jobId?include_data=true; successful API responses are
 * never fulfilled by the test.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { submitScrape, loginAgainstRealBackend } from '../helpers/api';
import { waitForJobCompletion } from '../helpers/jobs';
import { DEFAULT_INPUT_DIR, seedInputFiles } from '../helpers/fixtures';

function batchDetailPattern(jobId: string): RegExp {
	return new RegExp(`/api/v1/batch/${jobId.replaceAll('-', '\\-')}\\?include_data=true`);
}

async function createCompletedJob(request: APIRequestContext, movieId: string): Promise<string> {
	const fileName = `${movieId}.mp4`;
	await seedInputFiles([fileName]);
	const jobId = await submitScrape(request, {
		files: [`${DEFAULT_INPUT_DIR}/${fileName}`],
		selectedScrapers: ['e2emock'],
	});
	await waitForJobCompletion(request, jobId, { expectStatus: 'completed' });
	return jobId;
}

async function navigateClientSideToReview(page: Page, jobId: string): Promise<void> {
	// Register the URL waiter before clicking: under CI the click can commit the
	// route before page.evaluate resolves, which would otherwise miss the event.
	// Client-side navigation is committed before the new route's data is
	// necessarily settled. Waiting for the document `load` event can deadlock
	// this regression because it intentionally holds the previous route's
	// request until navigation has committed and teardown can abort it.
	await Promise.all([
		page.waitForURL(`**/review/${jobId}`, { waitUntil: 'commit' }),
		page.evaluate((url) => {
			const existing = document.querySelector('[data-review-test-nav]');
			existing?.remove();
			const link = document.createElement('a');
			link.dataset.reviewTestNav = 'true';
			link.href = url;
			link.textContent = 'test review navigation';
			document.body.append(link);
			link.click();
		}, `/review/${jobId}`),
	]);
}

test.describe('review load recovery', () => {
	test('renders a retryable error instead of permanent loading on HTTP 500', async ({ page }) => {
		const jobId = '00000000-0000-4000-8000-000000000500';
		const pattern = batchDetailPattern(jobId);
		await page.route(pattern, (route) =>
			route.fulfill({
				status: 500,
				contentType: 'application/json',
				body: JSON.stringify({ error: 'simulated review failure' }),
			}),
		);

		try {
			await page.goto(`/review/${jobId}`, { waitUntil: 'domcontentloaded' });
			await expect(page.getByRole('alert')).toBeVisible();
			await expect(page.getByText('Loading batch job...')).not.toBeVisible();
			await expect(page.getByRole('button', { name: 'Retry load' })).toBeVisible();
		} finally {
			await page.unroute(pattern);
		}
	});

	test('renders the not-found recovery state instead of permanent loading', async ({ page }) => {
		const jobId = '00000000-0000-4000-8000-000000000404';
		const pattern = batchDetailPattern(jobId);
		await page.route(pattern, (route) =>
			route.fulfill({
				status: 404,
				contentType: 'application/json',
				body: JSON.stringify({
					error: 'Job not found',
					code: 'JOB_NOT_FOUND',
					params: { job_id: jobId },
				}),
			}),
		);

		try {
			await page.goto(`/review/${jobId}`, { waitUntil: 'domcontentloaded' });
			await expect(page.getByRole('alert')).toBeVisible();
			await expect(page.getByText('Loading batch job...')).not.toBeVisible();
			await expect(page.getByRole('button', { name: 'Back to Browse' })).toBeVisible();
		} finally {
			await page.unroute(pattern);
		}
	});

	test('cancels A, leaves B uncanceled, and sends exactly one fresh A request on re-entry', async ({
		page,
		request,
	}) => {
		await loginAgainstRealBackend(request);
		const aJobId = await createCompletedJob(request, 'GOOD-911');
		const bJobId = await createCompletedJob(request, 'GOOD-912');
		const aPattern = batchDetailPattern(aJobId);
		let releaseA!: () => void;
		const holdA = new Promise<void>((resolve) => {
			releaseA = resolve;
		});
		let holdFirstA = true;
		let aRequests = 0;
		let aFailed = false;
		page.on('request', (req) => {
			if (req.url().match(aPattern)) aRequests += 1;
		});
		page.on('requestfailed', (req) => {
			if (req.url().match(aPattern)) aFailed = true;
		});
		await page.route(aPattern, async (route) => {
			if (holdFirstA) {
				holdFirstA = false;
				await holdA;
			}
			try {
				await route.continue();
			} catch {
				// The initial A request is expected to be aborted by route teardown.
			}
		});

		try {
			await page.goto(`/review/${aJobId}`, { waitUntil: 'domcontentloaded' });
			await expect(page.getByText('Loading batch job...')).toBeVisible();

			await navigateClientSideToReview(page, bJobId);
			releaseA();
			await expect.poll(() => aFailed, { timeout: 10_000 }).toBe(true);
			await expect(page.getByText('GOOD-912').first()).toBeVisible();

			await navigateClientSideToReview(page, aJobId);
			await expect(page.getByText('GOOD-911').first()).toBeVisible();
			await expect.poll(() => aRequests, { timeout: 10_000 }).toBe(2);
		} finally {
			releaseA();
			await page.unroute(aPattern);
		}
	});
});
