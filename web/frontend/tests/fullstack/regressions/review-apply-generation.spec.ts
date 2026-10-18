/**
 * Real-server admission regression, included by the normal fullstack CI job.
 * Scrape/apply/receipts/409 are real Go HTTP + worker + SQLite behavior with the
 * existing synthetic scraper. Only the cloned tab's GET job detail replays a
 * previously captured real response to model a deterministically stale view.
 * No mutation response is mocked. No live scraper or external media is needed.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';
import { rm } from 'node:fs/promises';
import { join } from 'node:path';
import type { BatchJobResponse, OrganizeRequest } from '../../../src/lib/api/types';
import { BACKEND_BASE, loginAgainstRealBackend, submitScrape } from '../helpers/api';
import { waitForJobCompletion } from '../helpers/jobs';
import { DEFAULT_INPUT_DIR, DEFAULT_OUTPUT_DIR, seedInputFiles } from '../helpers/fixtures';

async function readJob(request: APIRequestContext, jobId: string): Promise<BatchJobResponse> {
	const response = await request.get(`${BACKEND_BASE}/api/v1/batch/${jobId}?include_data=true`);
	expect(response.status(), await response.text()).toBe(200);
	return response.json() as Promise<BatchJobResponse>;
}

async function waitForFailedApply(
	request: APIRequestContext,
	jobId: string,
	generation: number,
	source: string,
) {
	// Include generation AND row status: a scrape's old terminal envelope is not
	// evidence that the requested apply has finished.
	await expect
		.poll(
			async () => {
				const snapshot = await readJob(request, jobId);
				return {
					generation: snapshot.apply_generation,
					status: snapshot.status,
					fileStatus: snapshot.results[source]?.status,
				};
			},
			{
				timeout: 30_000,
				message: `generation ${generation} must settle with the real missing-source failure`,
			},
		)
		.toEqual({ generation, status: 'completed', fileStatus: 'failed' });
	const snapshot = await readJob(request, jobId);
	expect(snapshot.results[source].error).toBeTruthy();
	expect(snapshot.results[source].movie).toBeTruthy();
	return snapshot;
}

test('copied recovery receipt sends its original generation and the real server rejects it after a competing apply', async ({
	page,
	context,
	browser,
	baseURL,
	request,
}) => {
	await loginAgainstRealBackend(request);
	const filename = 'GOOD-981.mp4';
	const source = join(DEFAULT_INPUT_DIR, filename);
	await seedInputFiles([filename]);
	const jobId = await submitScrape(request, { files: [source] });
	await waitForJobCompletion(request, jobId, { expectStatus: 'completed' });
	const destination = join(DEFAULT_OUTPUT_DIR, `receipt-${jobId}`);
	const key = `javinizer.review.applyRecovery.${jobId}`;
	const endpoint = `/api/v1/batch/${jobId}/organize`;
	const otherContext = await browser.newContext({
		baseURL,
		storageState: await context.storageState(),
	});

	try {
		await page.goto(`/review/${jobId}?view=detail`);
		await page.getByLabel('File operation', { exact: true }).selectOption('copy');
		await page.getByPlaceholder('Enter destination path (e.g., /path/to/output)').fill(destination);
		await page.getByRole('checkbox', { name: /Skip NFO Generation/ }).check();
		await page.getByRole('checkbox', { name: /Skip Media Download/ }).check();
		// Delete only this test's disposable input after scraping. This creates a
		// real per-file apply failure while retaining review metadata for retry.
		await rm(source);
		const acceptedResponse = page.waitForResponse(
			(response) =>
				new URL(response.url()).pathname === endpoint && response.request().method() === 'POST',
		);
		await page
			.getByRole('button', { name: /^Organize 1 File$/ })
			.first()
			.click();
		const response = await acceptedResponse;
		expect(response.status(), await response.text()).toBe(200);
		const receipt = (await response.json()) as { apply_generation: number };
		expect(Number.isSafeInteger(receipt.apply_generation)).toBe(true);
		expect(receipt.apply_generation).toBeGreaterThan(0);
		const originalRequest = response.request().postDataJSON() as OrganizeRequest;
		expect(originalRequest).not.toHaveProperty('expected_apply_generation');
		expect(originalRequest).toMatchObject({
			destination,
			copy_only: true,
			skip_nfo: true,
			skip_download: true,
		});
		const failedSnapshot = await waitForFailedApply(
			request,
			jobId,
			receipt.apply_generation,
			source,
		);
		await expect(page.getByRole('button', { name: 'Retry Failed', exact: true })).toBeEnabled();
		const originalRecord = await page.evaluate((key) => sessionStorage.getItem(key), key);
		expect(originalRecord).not.toBeNull();
		expect(JSON.parse(originalRecord!)).toMatchObject({
			acceptedApplyGeneration: receipt.apply_generation,
			organizeOperation: 'copy',
			destination,
		});

		const clone = await otherContext.newPage();
		const detailPattern = `**/api/v1/batch/${jobId}?include_data=true`;
		let staleReads = 0;
		await clone.route(detailPattern, async (route) => {
			expect(route.request().method()).toBe('GET');
			staleReads += 1;
			await route.fulfill({ json: failedSnapshot });
		});
		await clone.goto(`/review/${jobId}?view=detail`);
		expect(await clone.evaluate((key) => sessionStorage.getItem(key), key)).toBeNull();
		// Explicitly model a duplicated tab that copied a once-valid receipt. Do
		// not use addInitScript: it would silently reseed authority after rejection.
		await clone.evaluate(({ key, record }) => sessionStorage.setItem(key, record!), {
			key,
			record: originalRecord,
		});
		await clone.reload();
		const retry = clone.getByRole('button', { name: 'Retry Failed', exact: true });
		await expect(retry).toBeEnabled();
		expect(staleReads).toBeGreaterThanOrEqual(2);

		// A second authenticated session explicitly applies the failed path. Both
		// this acceptance and the following stale rejection go to the real server.
		const competing = await otherContext.request.post(endpoint, {
			data: { ...originalRequest, retry_file_paths: [source] },
		});
		expect(competing.status(), await competing.text()).toBe(200);
		const newerReceipt = (await competing.json()) as { apply_generation: number };
		expect(Number.isSafeInteger(newerReceipt.apply_generation)).toBe(true);
		expect(newerReceipt.apply_generation).toBeGreaterThan(receipt.apply_generation);
		const beforeRejection = await waitForFailedApply(
			request,
			jobId,
			newerReceipt.apply_generation,
			source,
		);
		await expect(retry).toBeEnabled(); // This client still has its captured N view.
		const rejectedResponse = clone.waitForResponse(
			(result) =>
				new URL(result.url()).pathname === endpoint && result.request().method() === 'POST',
		);
		await retry.click();
		const rejected = await rejectedResponse;
		expect(rejected.request().postDataJSON()).toEqual({
			...originalRequest,
			retry_file_paths: [source],
			expected_apply_generation: receipt.apply_generation,
		});
		expect(rejected.status(), await rejected.text()).toBe(409);
		expect(await rejected.json()).toMatchObject({ code: 'APPLY_GENERATION_MISMATCH' });
		await expect.poll(() => clone.evaluate((key) => sessionStorage.getItem(key), key)).toBeNull();
		await expect(retry.and(clone.locator(':enabled'))).toHaveCount(0);
		await clone.reload();
		await expect(clone.getByRole('tab', { name: /^Failed/ })).toHaveAttribute(
			'aria-selected',
			'true',
		);
		await expect(retry.and(clone.locator(':enabled'))).toHaveCount(0);
		expect(await clone.evaluate((key) => sessionStorage.getItem(key), key)).toBeNull();
		expect(await page.evaluate((key) => sessionStorage.getItem(key), key)).toBe(originalRecord);
		const afterRejection = await readJob(request, jobId);
		expect(afterRejection.apply_generation).toBe(newerReceipt.apply_generation);
		expect(afterRejection.results).toEqual(beforeRejection.results);
		expect(afterRejection.operation_count).toBe(beforeRejection.operation_count);
		expect(afterRejection.apply_plan).toEqual(beforeRejection.apply_plan);
	} finally {
		await otherContext.close();
		await rm(source, { force: true });
		await rm(destination, { recursive: true, force: true });
	}
});
