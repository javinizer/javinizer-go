import { test, expect } from '@playwright/test';
import { submitScrape, loginAgainstRealBackend } from '../helpers/api';
import { waitForJobCompletion } from '../helpers/jobs';
import { DEFAULT_INPUT_DIR, seedInputFiles } from '../helpers/fixtures';

function batchDetailPattern(jobId: string): RegExp {
	return new RegExp(`/api/v1/batch/${jobId.replaceAll('-', '\\-')}\\?include_data=true`);
}

test('times out a held review request and retries successfully without restart', async ({ page, request }) => {
	await loginAgainstRealBackend(request);
	const movieId = 'GOOD-913';
	const fileName = `${movieId}.mp4`;
	await seedInputFiles([fileName]);
	const jobId = await submitScrape(request, {
		files: [`${DEFAULT_INPUT_DIR}/${fileName}`],
		selectedScrapers: ['e2emock'],
	});
	await waitForJobCompletion(request, jobId, { expectStatus: 'completed' });

	const pattern = batchDetailPattern(jobId);
	let releaseFirst!: () => void;
	const firstRequestGate = new Promise<void>((resolve) => {
		releaseFirst = resolve;
	});
	let requests = 0;
	let holdFirst = true;
	await page.route(pattern, async (route) => {
		requests += 1;
		if (holdFirst) {
			holdFirst = false;
			await firstRequestGate;
		}
		try {
			await route.continue();
		} catch {
			// The first request is expected to be aborted by the client timeout.
		}
	});

	try {
		await page.goto(`/review/${jobId}`, { waitUntil: 'domcontentloaded' });
		await expect(page.getByRole('alert')).toBeVisible({ timeout: 10_000 });
		await expect(page.getByText('Loading batch job...')).not.toBeVisible();

		releaseFirst();
		await page.getByRole('button', { name: 'Retry load' }).click();
		await expect(page.getByText(movieId).first()).toBeVisible({ timeout: 10_000 });
		expect(requests).toBe(2);
	} finally {
		releaseFirst();
		await page.unroute(pattern);
	}
});
