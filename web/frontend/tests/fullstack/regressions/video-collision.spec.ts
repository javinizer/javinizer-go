import { test, expect } from '@playwright/test';
import {
	mkdirSync,
	mkdtempSync,
	writeFileSync,
	readFileSync,
	existsSync,
	lstatSync,
} from 'node:fs';
import { dirname, join } from 'node:path';
import { DEFAULT_INPUT_DIR, DEFAULT_OUTPUT_DIR } from '../helpers';

test('skipped duplicate remains actionable after review refresh and reload', async ({
	page,
	request,
}) => {
	mkdirSync(DEFAULT_INPUT_DIR, { recursive: true });
	mkdirSync(DEFAULT_OUTPUT_DIR, { recursive: true });
	const input = mkdtempSync(join(DEFAULT_INPUT_DIR, 'duplicate-collision-'));
	const output = mkdtempSync(join(DEFAULT_OUTPUT_DIR, 'duplicate-collision-'));
	const sources = ['a', 'b'].map((part) => {
		const source = join(input, part, 'GOOD-27790.mp4');
		mkdirSync(dirname(source), { recursive: true });
		writeFileSync(source, `synthetic duplicate ${part}`);
		return source;
	});
	const created = await request.post('/api/v1/batch/scrape', {
		data: {
			files: sources,
			selected_scrapers: ['e2emock'],
			destination: output,
			operation_mode: 'organize',
		},
	});
	expect(created.ok(), await created.text()).toBeTruthy();
	const { job_id: jobId } = await created.json();
	await expect
		.poll(async () => {
			const response = await request.get(`/api/v1/batch/${jobId}?include_data=true`);
			return (await response.json()).status;
		})
		.toBe('completed');
	await page.goto(`/review/${jobId}`);
	await page.getByRole('button', { name: /^Detail$/i }).click();
	await page
		.locator('label')
		.filter({ hasText: 'Skip Media Download' })
		.locator('input[type=checkbox]')
		.check();
	const panel = page.getByTestId('video-collision-review');
	await panel.getByRole('button', { name: 'Review / refresh destinations' }).click();
	await expect(panel).toContainText('Unresolved 1');
	await panel.getByRole('combobox').selectOption('skip');
	await expect(panel).toContainText('Skip 1');
	await expect(panel).toContainText('Unresolved 0');
	await panel.getByRole('button', { name: 'Review / refresh destinations' }).click();
	await expect(panel.getByRole('combobox')).toHaveValue('skip');
	await page.reload();
	await page
		.locator('label')
		.filter({ hasText: 'Skip Media Download' })
		.locator('input[type=checkbox]')
		.check();
	await panel.getByRole('button', { name: 'Review / refresh destinations' }).click();
	await expect(panel.getByRole('combobox')).toHaveValue('skip');
	await expect(panel).toContainText('Skip 1');
	await panel.getByRole('combobox').selectOption('fail');
	await expect(panel).toContainText('Unresolved 1');
	await expect(panel).toContainText('Skip 0');
	await expect(panel.getByRole('combobox')).toHaveValue('fail');
	for (const [index, source] of sources.entries())
		expect(readFileSync(source, 'utf8')).toBe(`synthetic duplicate ${['a', 'b'][index]}`);
});

for (const operation of ['copy', 'move'] as const) {
	for (const policy of ['skip', 'replace'] as const) {
		for (const equalContent of [false, true]) {
			test(`video collision ${operation}/${policy}/${equalContent ? 'equal' : 'different'}`, async ({
				page,
				request,
			}) => {
				test.setTimeout(90_000);
				mkdirSync(DEFAULT_INPUT_DIR, { recursive: true });
				mkdirSync(DEFAULT_OUTPUT_DIR, { recursive: true });
				const input = mkdtempSync(join(DEFAULT_INPUT_DIR, 'collision-'));
				const output = mkdtempSync(join(DEFAULT_OUTPUT_DIR, 'collision-'));
				const id = `GOOD-${27710 + (operation === 'move' ? 4 : 0) + (policy === 'replace' ? 2 : 0) + Number(equalContent)}`;
				const filename = `${id} original.mp4`;
				const source = join(input, filename);
				const incoming = `incoming ${id}`;
				const previous = equalContent ? incoming : `previous ${id}`;
				writeFileSync(source, incoming);
				await page.goto('/browse');
				await page.waitForLoadState('networkidle');
				const inputBox = page.getByPlaceholder('Enter path (e.g., /path/to/videos)').first();
				await inputBox.fill(input);
				await inputBox.press('Enter');
				await page.getByText(filename, { exact: true }).first().click();
				const destination = page
					.getByPlaceholder('Enter destination path (e.g., /path/to/output)')
					.first();
				await destination.fill(output);
				await destination.press('Tab');
				const scrape = page.waitForResponse(
					(response) =>
						response.request().method() === 'POST' &&
						new URL(response.url()).pathname === '/api/v1/batch/scrape',
				);
				await page.getByRole('button', { name: /Scrape 1 File/i }).click();
				const scrapeResponse = await scrape;
				expect(scrapeResponse.ok()).toBeTruthy();
				const { job_id: jobId } = await scrapeResponse.json();
				await page.waitForURL(new RegExp(`/review/${jobId}`));
				await page.getByRole('button', { name: /^Detail$/i }).click();
				await page.locator('#organizeOperation').selectOption(operation);
				await page
					.locator('label')
					.filter({ hasText: 'Skip Media Download' })
					.locator('input[type="checkbox"]')
					.check();
				const jobResponse = await request.get(`/api/v1/batch/${jobId}?include_data=true`);
				const job = await jobResponse.json();
				const resultSource = Object.keys(job.results)[0];
				const result = Object.values(job.results)[0] as { result_id: string };
				const previewResponse = await request.post(
					`/api/v1/batch/${jobId}/results/${result.result_id}/preview`,
					{ data: { destination: output, operation_mode: 'organize', skip_download: true } },
				);
				expect(previewResponse.ok(), await previewResponse.text()).toBeTruthy();
				const preview = await previewResponse.json();
				const target = preview.full_path as string;
				mkdirSync(dirname(target), { recursive: true });
				writeFileSync(target, previous);
				const priorInode = lstatSync(target, { bigint: true }).ino;
				const panel = page.getByTestId('video-collision-review');
				await panel.getByRole('button', { name: /Review \/ refresh destinations/i }).click();
				await expect(panel).toContainText(source);
				await expect(panel).toContainText(target);
				await expect(panel).toContainText('Unresolved 1');
				await expect(
					page.getByRole('button', { name: /^Organize 1 File$/i }).first(),
				).toBeDisabled();
				const refused = await request.post(`/api/v1/batch/${jobId}/organize`, {
					data: {
						destination: output,
						copy_only: operation === 'copy',
						skip_nfo: true,
						skip_download: true,
					},
				});
				expect(refused.status()).toBe(409);
				await panel.getByRole('combobox').selectOption(policy);
				await expect(panel).toContainText('Unresolved 0');
				await page.reload();
				await page.waitForLoadState('networkidle');
				await page.locator('#organizeOperation').selectOption(operation);
				await page
					.locator('label')
					.filter({ hasText: 'Skip Media Download' })
					.locator('input[type="checkbox"]')
					.check();
				await panel.getByRole('button', { name: /Review \/ refresh destinations/i }).click();
				await expect(panel.getByRole('combobox')).toHaveValue(policy);
				await expect(panel).toContainText('Unresolved 0');
				const apply = page.waitForResponse(
					(response) =>
						response.request().method() === 'POST' &&
						new URL(response.url()).pathname === `/api/v1/batch/${jobId}/organize`,
				);
				await page
					.getByRole('button', { name: /^Organize 1 File$/i })
					.first()
					.click();
				const accepted = await apply;
				expect(accepted.ok(), await accepted.text()).toBeTruthy();
				let completed: any;
				await expect
					.poll(
						async () => {
							const response = await request.get(`/api/v1/batch/${jobId}?include_data=true`);
							completed = await response.json();
							return completed.results[resultSource]?.video_outcome;
						},
						{ timeout: 30_000 },
					)
					.toBe(policy === 'skip' ? 'skipped' : 'replaced');
				expect(completed.failed).toBe(0);
				expect(readFileSync(target, 'utf8')).toBe(policy === 'skip' ? previous : incoming);
				expect(existsSync(source)).toBe(policy === 'skip' || operation === 'copy');
				if (policy === 'skip') expect(lstatSync(target, { bigint: true }).ino).toBe(priorInode);
				await page.goto(`/jobs/${jobId}`);
				await expect(
					page.getByText(
						policy === 'skip' ? 'Entire Apply skipped; files unchanged' : 'Reviewed video replaced',
						{ exact: false },
					),
				).toBeVisible();
				if (policy === 'skip')
					await expect(page.getByRole('button', { name: /Revert/i })).toHaveCount(0);
			});
		}
	}
}
