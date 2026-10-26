/** Investigation coverage for the reported Docker automatic completion redirect.
 * Real production static assets + real API/e2emock; no successful API mocks.
 * Not locally executed: Chromium startup is blocked by this executor's socket policy.
 */
import { test, expect } from '@playwright/test';
import { mkdir, mkdtemp, writeFile, readFile } from 'node:fs/promises';
import { join } from 'node:path';

const inputRoot = process.env.JAVINIZER_E2E_INPUT_DIR ?? '/tmp/javinizer-e2e-static-input';
const outputRoot = process.env.JAVINIZER_E2E_OUTPUT_DIR ?? '/tmp/javinizer-e2e-static-output';
const sourcePlaceholder = 'Enter path (e.g., /path/to/videos)';

for (const navigation of ['sibling-typed'] as string[]) {
	for (const operation of ['leave-in-place', 'organize'] as const) {
		test(`Jobs / ${operation}: automatic completed-apply redirect restores this job source instead of another Browse location`, async ({
			page,
		}, testInfo) => {
			test.setTimeout(120_000);
			await mkdir(inputRoot, { recursive: true });
			await mkdir(outputRoot, { recursive: true });
			const root = await mkdtemp(join(inputRoot, 'browse-return-'));
			const sourceName = navigation === 'child' ? 'nested' : 'new-folder';
			const source = join(root, sourceName);
			const bootstrapPath = navigation === 'child' ? root : join(root, 'old-folder');
			if (navigation !== 'child') {
				await mkdir(bootstrapPath);
				await writeFile(join(bootstrapPath, 'OLD-001.mp4'), 'synthetic-old-location');
			}
			const destination = await mkdtemp(join(outputRoot, 'browse-return-'));
			await mkdir(source);
			await writeFile(join(source, 'GOOD-001.mp4'), 'synthetic-browser-fixture');
			// A non-video marker preserves the source directory even when organizing.
			await writeFile(join(source, 'fixture-marker.txt'), 'private disposable test fixture');
			await writeFile(
				join(source, 'GOOD-001.nfo'),
				'<?xml version="1.0"?><movie><title>Synthetic existing metadata</title><id>GOOD-001</id><uniqueid type="javinizer" default="true">GOOD-001</uniqueid></movie>',
			);
			const login = await page.request.post('/api/v1/auth/login', {
				data: { username: 'admin', password: 'adminpassword123' },
			});
			expect(login.ok()).toBe(true);
			await page.goto('/browse');
			const path = page.getByPlaceholder(sourcePlaceholder).first();
			await path.fill(bootstrapPath);
			await path.press('Enter');
			await expect(path).toHaveValue(bootstrapPath);
			await expect
				.poll(
					async () =>
						(await page.context().cookies()).find(
							(cookie) => cookie.name === 'javinizer_browse_bootstrap',
						)?.value,
				)
				.toContain(encodeURIComponent(bootstrapPath));
			// Create a genuinely stale retained root-layout bootstrap. A fresh
			// context alone has no bootstrap and would miss the original bug.
			await page.reload();
			await expect(path).toHaveValue(bootstrapPath);
			expect(
				await page.evaluate(
					() =>
						(
							window as unknown as {
								__JAVINIZER_SSR__: { browseBootstrap: { initialPath: string } };
							}
						).__JAVINIZER_SSR__.browseBootstrap.initialPath,
				),
			).toBe(bootstrapPath);
			if (navigation !== 'child') {
				await expect(page.getByRole('button', { name: /OLD-001\.mp4/ })).toBeVisible();
			}
			if (navigation === 'sibling-typed') {
				await path.fill(source);
				await path.press('Enter');
			} else {
				if (navigation === 'sibling-up') {
					await page.getByTitle('Go up', { exact: true }).click();
					await expect(path).toHaveValue(root);
				}
				await page.getByRole('button', { name: new RegExp(sourceName) }).click();
			}
			await expect(path).toHaveValue(source);
			await expect(page.getByText('fixture-marker.txt', { exact: true })).toBeVisible();
			await expect(page.getByText('OLD-001.mp4', { exact: true })).toHaveCount(0);
			expect(await page.evaluate(() => localStorage.getItem('javinizer_input_path'))).toBe(source);
			await expect
				.poll(
					async () =>
						(await page.context().cookies()).find(
							(cookie) => cookie.name === 'javinizer_browse_bootstrap',
						)?.value,
				)
				.toContain(encodeURIComponent(source));
			await testInfo.attach('before-apply-location.json', {
				body: JSON.stringify(
					await page.evaluate(() => ({
						input: localStorage.getItem('javinizer_input_path'),
						cookie: document.cookie
							.split('; ')
							.find((entry) => entry.startsWith('javinizer_browse_bootstrap=')),
						injected: (window as unknown as { __JAVINIZER_SSR__: unknown }).__JAVINIZER_SSR__,
					})),
					null,
					2,
				),
				contentType: 'application/json',
			});
			await page
				.getByText(
					operation === 'leave-in-place'
						? 'Leave video files in place'
						: 'Organize into another location',
					{ exact: true },
				)
				.click();
			// Avoid apply-download retries: e2emock artwork URLs are intentionally invalid.
			await page.getByText('Skip media', { exact: true }).click();
			await expect(page.getByRole('radio', { name: 'Skip media', exact: true })).toBeChecked();
			if (operation === 'organize') {
				await page
					.getByPlaceholder('Enter destination path (e.g., /path/to/output)')
					.fill(destination);
			}
			await page.getByRole('button', { name: /GOOD-001\.mp4/ }).click();
			await page
				.getByRole('button', {
					name: operation === 'leave-in-place' ? /^Update 1 File$/i : /^Scrape 1 File$/i,
				})
				.click();
			await page.waitForURL(/\/review\/[^/?]+/, { timeout: 30_000 });
			const reviewPath = new URL(page.url()).pathname;
			const jobId = reviewPath.split('/').pop()!;
			// Leave this completed scrape unapplied, browse elsewhere, then reopen
			// the original job using the actual Jobs button (not a direct Review URL).
			await page.goto('/browse');
			await path.fill(bootstrapPath);
			await path.press('Enter');
			await expect(page.getByRole('button', { name: /OLD-001\.mp4/ })).toBeVisible();
			expect(await page.evaluate(() => localStorage.getItem('javinizer_input_path'))).toBe(bootstrapPath);
			await page.goto('/jobs');
			const jobCard = page.locator('.group').filter({ has: page.getByText(jobId.slice(0, 8), { exact: true }) });
			await jobCard.getByRole('button', { name: /Review.*Organize/i }).click();
			await page.waitForURL(new RegExp(`/review/${jobId}`));
			await page.getByRole('button', { name: /^Detail$/i }).click();
			// Still stale before Apply: merely reviewing must not mutate Browse.
			expect(await page.evaluate(() => localStorage.getItem('javinizer_input_path'))).toBe(bootstrapPath);
			await page
				.getByRole('button', {
					name: operation === 'leave-in-place' ? /^Update 1 File$/i : /^Organize 1 File$/i,
				})
				.first()
				.click();
			// Do NOT click Continue/Back or call page.goto: exercise the real
			// completion handler and its delayed goto('/browse') navigation.
			await page.waitForURL(/\/browse$/, { timeout: 45_000 });
			await expect(path).toHaveValue(source);
			await expect(page.getByText('fixture-marker.txt', { exact: true })).toBeVisible();
			await expect(page.getByText('OLD-001.mp4', { exact: true })).toHaveCount(0);
			expect(await page.evaluate(() => localStorage.getItem('javinizer_input_path'))).toBe(source);
			await page.screenshot({
				path: testInfo.outputPath(`${operation}-automatic-return.png`),
				fullPage: true,
			});
			if (operation === 'leave-in-place') {
				expect(await readFile(join(source, 'GOOD-001.mp4'), 'utf8')).toBe(
					'synthetic-browser-fixture',
				);
			}
			await page.goBack();
			await expect.poll(() => new URL(page.url()).pathname).toBe(reviewPath);
			await page.goForward();
			await expect(page).toHaveURL(/\/browse$/);
			await expect(path).toHaveValue(source);
			await page.reload();
			await expect(path).toHaveValue(source);
			await expect(page.getByText('fixture-marker.txt', { exact: true })).toBeVisible();
			await expect(page.getByText('OLD-001.mp4', { exact: true })).toHaveCount(0);
			await page.reload();
			await expect(path).toHaveValue(source);
			await expect(page.getByText('fixture-marker.txt', { exact: true })).toBeVisible();
			await expect(page.getByText('OLD-001.mp4', { exact: true })).toHaveCount(0);
			// Retain only this test's fresh directories as diagnostic evidence.
		});
	}
}
