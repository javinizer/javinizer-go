/** Issue #277 diagnostic, real Browse -> Review -> apply. No successful API mocks.
 * Synthetic bytes only. Run only this spec, using playwright.issue277.config.ts.
 * We observe the default Browse media policy, then explicitly skip downloads in
 * Review so this video-collision probe never needs external media providers.
 */
import { test as base, expect } from '@playwright/test';
import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync, linkSync } from 'node:fs';
import { dirname, isAbsolute, join, relative, resolve } from 'node:path';
import { DEFAULT_INPUT_DIR, DEFAULT_OUTPUT_DIR } from '../helpers';

// Abort external requests; never fulfill/mutate a successful API response.
// Install on the context before the page exists, covering popups as well.
function isFixtureURL(raw: string): boolean {
	const url = new URL(raw);
	// Prevent remote image proxy requests before they can reach the backend.
	if (url.pathname === '/api/v1/temp/image' && url.searchParams.has('url')) {
		try {
			if (!isFixtureURL(url.searchParams.get('url')!)) return false;
		} catch {
			return false;
		}
	}
	return (
		['http:', 'https:', 'ws:', 'wss:'].includes(url.protocol) &&
		['localhost', '127.0.0.1', '[::1]'].includes(url.hostname) &&
		['5175', '18080'].includes(url.port)
	);
}
const test = base.extend<{ guardEgress: void }>({
	guardEgress: [
		async ({ context }, use, testInfo) => {
			const blocked: string[] = [];
			await context.route('**/*', (route) => {
				if (isFixtureURL(route.request().url())) return route.continue();
				blocked.push(
					new URL(route.request().url()).origin + new URL(route.request().url()).pathname,
				);
				return route.abort('blockedbyclient');
			});
			await context.routeWebSocket('**/*', (route) => {
				if (isFixtureURL(route.url())) {
					route.connectToServer();
					return;
				}
				blocked.push(new URL(route.url()).origin + new URL(route.url()).pathname);
				route.close({ code: 1008, reason: 'Diagnostic permits loopback only' });
			});
			await use();
			await testInfo.attach('blocked-external-browser-requests', {
				body: JSON.stringify(blocked, null, 2),
				contentType: 'application/json',
			});
		},
		{ auto: true },
	],
});

type Fixture = 'absent' | 'different-bytes' | 'same-bytes' | 'hardlink';
const fixtures: Fixture[] = ['absent', 'different-bytes', 'same-bytes', 'hardlink'];
const snapshot = (path: string) => ({
	path,
	exists: existsSync(path),
	sha256: existsSync(path) ? createHash('sha256').update(readFileSync(path)).digest('hex') : null,
});
const sourcePlaceholder = 'Enter path (e.g., /path/to/videos)';
const destinationPlaceholder = 'Enter destination path (e.g., /path/to/output)';

for (const operation of ['move', 'copy'] as const) {
	for (const fixture of fixtures) {
		test(`${operation}: ${fixture} target`, async ({ page, request }, testInfo) => {
			test.setTimeout(90_000);
			const evidence: Record<string, unknown> = {
				revision: process.env.ISSUE277_SOURCE_REVISION ?? 'unspecified',
				platform: process.platform,
				operation,
				fixture,
				limitation:
					'Synthetic video bytes; real browser/API/workflow/filesystem. Media downloads explicitly skipped in Review.',
				posts: [],
			};
			let source = '';
			let target = '';
			page.on('request', (req) => {
				if (req.method() === 'POST' && new URL(req.url()).pathname.startsWith('/api/v1/batch/')) {
					(evidence.posts as unknown[]).push({
						path: new URL(req.url()).pathname,
						body: req.postDataJSON(),
					});
				}
			});
			try {
				mkdirSync(DEFAULT_INPUT_DIR, { recursive: true });
				mkdirSync(DEFAULT_OUTPUT_DIR, { recursive: true });
				const input = mkdtempSync(join(DEFAULT_INPUT_DIR, 'issue277-'));
				const output = mkdtempSync(join(DEFAULT_OUTPUT_DIR, 'issue277-'));
				const id = `GOOD-${27700 + fixtures.indexOf(fixture) + (operation === 'copy' ? 10 : 0)}`;
				const filename = `${id} original.mp4`;
				source = join(input, filename);
				const sourceBytes = Buffer.from(`issue277 source ${id}\n`);
				writeFileSync(source, sourceBytes);
				// Each Playwright test has a fresh context. Clear app state once before
				// Browse hydration, without changing the real authentication cookies.
				await page.goto('/');
				await page.evaluate(() => {
					localStorage.clear();
					sessionStorage.clear();
				});
				await page.goto('/browse');
				await page.waitForLoadState('networkidle');
				await expect(page.locator('input[name="video-operation"][value="organize"]')).toBeChecked();
				const inputBox = page.getByPlaceholder(sourcePlaceholder).first();
				await inputBox.fill(input);
				await inputBox.press('Enter');
				await page.getByText(filename, { exact: true }).first().click();
				const destinationBox = page.getByPlaceholder(destinationPlaceholder).first();
				await destinationBox.fill(output);
				await destinationBox.press('Tab');
				await page.screenshot({
					path: testInfo.outputPath('01-browse-defaults.png'),
					fullPage: true,
				});
				const scrapeResponsePromise = page.waitForResponse(
					(r) =>
						r.request().method() === 'POST' && new URL(r.url()).pathname === '/api/v1/batch/scrape',
				);
				await page.getByRole('button', { name: /Scrape 1 File/i }).click();
				const scrapeResponse = await scrapeResponsePromise;
				evidence.scrapeHTTPStatus = scrapeResponse.status();
				expect(scrapeResponse.ok(), await scrapeResponse.text()).toBeTruthy();
				const scrapePayload = scrapeResponse.request().postDataJSON();
				evidence.scrapePayload = scrapePayload;
				expect(scrapePayload).toMatchObject({
					update: false,
					operation_mode: 'organize',
					apply_plan: { video_operation: 'organize', nfo_output: 'write', media_policy: 'missing' },
				});
				const { job_id: jobId } = await scrapeResponse.json();
				await page.waitForURL(new RegExp(`/review/${jobId}(?:$|[?#])`), { timeout: 30_000 });
				await page.getByRole('button', { name: /^Detail$/i }).click();
				await expect(page.locator('body')).toContainText(id);
				await page.locator('#organizeOperation').selectOption(operation);
				await page.getByRole('button', { name: /^Options$/i }).click();
				await expect(page.getByText('Force Overwrite', { exact: true })).toHaveCount(0);
				await expect(page.getByText('Replace Existing Media', { exact: true })).toHaveCount(0);
				// Scope by the text unique to the destination card, not the second
				// Skip Downloads checkbox in the expanded Options section.
				await page
					.locator('label')
					.filter({ hasText: 'Skip Media Download' })
					.locator('input[type="checkbox"]')
					.check();
				await page.screenshot({
					path: testInfo.outputPath('02-review-options.png'),
					fullPage: true,
				});
				const beforeResponse = await request.get(`/api/v1/batch/${jobId}?include_data=true`);
				expect(beforeResponse.ok()).toBeTruthy();
				const beforeJob = await beforeResponse.json();
				const result = Object.values(beforeJob.results)[0] as { result_id: string };
				expect(result.result_id).toBeTruthy();
				const previewResponse = await request.post(
					`/api/v1/batch/${jobId}/results/${result.result_id}/preview`,
					{
						data: {
							destination: output,
							operation_mode: 'organize',
							skip_download: true,
							copy_only: operation === 'copy',
						},
					},
				);
				expect(previewResponse.ok(), await previewResponse.text()).toBeTruthy();
				const preview = await previewResponse.json();
				evidence.preview = preview;
				target = preview.full_path;
				expect(typeof target).toBe('string');
				expect(isAbsolute(target)).toBeTruthy();
				const targetRelative = relative(resolve(output), resolve(target));
				expect(
					targetRelative && !targetRelative.startsWith('..') && !isAbsolute(targetRelative),
					'preview target must stay within this disposable output fixture',
				).toBeTruthy();
				expect(resolve(target)).not.toBe(resolve(source));
				mkdirSync(dirname(target), { recursive: true });
				if (fixture === 'different-bytes')
					writeFileSync(target, 'issue277 existing distinct video\n');
				if (fixture === 'same-bytes') writeFileSync(target, sourceBytes);
				if (fixture === 'hardlink') linkSync(source, target);
				const before = { source: snapshot(source), target: snapshot(target) };
				evidence.before = before;
				const organizeResponsePromise = page.waitForResponse(
					(r) =>
						r.request().method() === 'POST' &&
						new URL(r.url()).pathname === `/api/v1/batch/${jobId}/organize`,
				);
				await page
					.getByRole('button', { name: /^Organize 1 File$/i })
					.first()
					.click();
				const organizeResponse = await organizeResponsePromise;
				evidence.organizeHTTPStatus = organizeResponse.status();
				evidence.organizeResponse = await organizeResponse.text();
				const organizePayload = organizeResponse.request().postDataJSON();
				evidence.organizePayload = organizePayload;
				expect(organizeResponse.ok()).toBeTruthy();
				expect(organizePayload).toMatchObject({
					copy_only: operation === 'copy',
					operation_mode: 'organize',
					skip_download: true,
					overrides: { overwrite_existing_media: false },
				});
				expect(organizePayload).not.toHaveProperty('force_update');
				expect(organizePayload.overrides).not.toHaveProperty('force_overwrite');
				let terminalJob: any;
				await expect
					.poll(
						async () => {
							const response = await request.get(`/api/v1/batch/${jobId}?include_data=true`);
							expect(response.ok()).toBeTruthy();
							terminalJob = await response.json();
							return (
								terminalJob.apply_generation > (beforeJob.apply_generation ?? 0) &&
								['completed', 'organized', 'failed'].includes(terminalJob.status)
							);
						},
						{ timeout: 30_000 },
					)
					.toBeTruthy();
				evidence.terminalJob = terminalJob;
				const finalResult = Object.values(terminalJob.results)[0] as {
					status: string;
					error?: string;
				};
				evidence.after = { source: snapshot(source), target: snapshot(target) };
				if (fixture === 'different-bytes' || fixture === 'same-bytes') {
					expect(finalResult.status).toBe('failed');
					if (process.env.ISSUE277_SOURCE_REVISION === '17092b84850713a3bee8c32390aa56b726942b5c') {
						expect(finalResult.error).toContain(
							'artifact publication failed: staged publication destination exists and overwrite is disabled:',
						);
					} else {
						// Baseline may refuse earlier with older wording; preserve its exact
						// error in evidence rather than requiring the newer text.
						expect(finalResult.error).toMatch(/exist|overwrite|conflict/i);
					}
					expect(finalResult.error).toContain(target);
					await expect(page.getByText(finalResult.error!, { exact: true }).first()).toBeVisible({
						timeout: 15_000,
					});
					expect(snapshot(source)).toEqual(before.source);
					expect(snapshot(target)).toEqual(before.target);
				} else {
					expect(finalResult.status, JSON.stringify(finalResult)).toBe('completed');
					expect(snapshot(target).sha256).toBe(before.source.sha256);
					if (operation === 'copy') expect(snapshot(source).sha256).toBe(before.source.sha256);
				}
				await page.screenshot({
					path: testInfo.outputPath('03-apply-outcome.png'),
					fullPage: true,
				});
			} finally {
				if (source) evidence.finalSource = snapshot(source);
				if (target && existsSync(target)) evidence.finalTarget = snapshot(target);
				evidence.browserURL = page.url();
				writeFileSync(
					testInfo.outputPath('issue277-evidence.json'),
					JSON.stringify(evidence, null, 2),
				);
				await testInfo.attach('issue277-evidence', {
					path: testInfo.outputPath('issue277-evidence.json'),
					contentType: 'application/json',
				});
			}
		});
	}
}
