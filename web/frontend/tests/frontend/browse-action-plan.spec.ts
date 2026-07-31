import { test, expect, type Page } from '@playwright/test';
import { setupMockRoutes } from './review-helpers';

const config = {
	output: { operation_mode: 'organize', folder_format: '', file_format: '<ID>', subfolder_format: [], download_cover: true, download_poster: true, download_trailer: true, download_extrafanart: true },
	api: { security: { allowed_directories: ['/videos', '/output'] } },
	metadata: { completeness: { enabled: false } }
};

async function setupBrowseRoutes(page: Page) {
	await page.route('**/api/v1/**', async (route) => {
		const url = new URL(route.request().url());
		if (url.pathname === '/api/v1/auth/status') return route.fulfill({ json: { initialized: true, authenticated: true, username: 'admin' } });
		if (url.pathname === '/api/v1/config') return route.fulfill({ json: config });
		if (url.pathname === '/api/v1/cwd') return route.fulfill({ json: { path: '/videos' } });
		if (url.pathname === '/api/v1/scrapers') return route.fulfill({ json: { scrapers: [{ name: 'mock', display_title: 'Mock', enabled: true }] } });
		if (url.pathname === '/api/v1/browse') return route.fulfill({ json: { current_path: '/videos', parent_path: '/', items: [{ name: 'IPX-123.mp4', path: '/videos/IPX-123.mp4', is_dir: false, size: 10, mod_time: '2026-01-01T00:00:00Z' }] } });
		if (url.pathname.startsWith('/api/v1/version')) return route.fulfill({ json: { current: 'test', update_available: false, source: 'disabled' } });
		return route.fulfill({ json: {} });
	});
}

test.describe('Browse canonical action plan', () => {
	test.beforeEach(async ({ page }) => {
		await setupBrowseRoutes(page);
		await page.goto('/browse');
		await page.waitForLoadState('networkidle');
	});

test('keeps the username-adjacent settings menu outside the scrolling nav clip', async ({ page }) => {
		const settings = page.getByRole('button', { name: 'Settings' });
		await settings.click();
		await expect(settings).toHaveAttribute('aria-expanded', 'true');
		const menu = page.locator('#navigation-settings-menu');
		await expect(menu).toBeVisible();
		await expect(menu.getByRole('link', { name: 'Logs' })).toBeVisible();
		await page.waitForTimeout(250);
		const geometry = await page.evaluate(() => {
			const trigger = document.querySelector('nav button[aria-label="Settings"]')!;
			const menu = document.getElementById('navigation-settings-menu')!;
			const nav = document.querySelector('nav')!;
			const menuRect = menu.getBoundingClientRect();
			const navRect = nav.getBoundingClientRect();
			return {
				insideScroller: !!trigger.closest('.overflow-x-auto'),
				width: menuRect.width,
				height: menuRect.height,
				bottom: menuRect.bottom,
				navBottom: navRect.bottom
			};
		});
		expect(geometry.insideScroller).toBe(false);
		expect(geometry.width).toBeGreaterThan(100);
		expect(geometry.height).toBeGreaterThan(80);
		expect(geometry.bottom).toBeGreaterThan(geometry.navBottom + 80);
		await page.keyboard.press('Escape');
		await expect(menu).toHaveCount(0);
	});
	test('covers four operations, contextual controls, summary, and destructive replacement', async ({ page }) => {
		const radios = page.getByRole('radio', { name: /Organize into another location|Rename in place|Rename video file only|Leave video files in place/ });
		await expect(radios).toHaveCount(4);
		await expect(page.getByRole('radio', { name: /Organize into another location/ })).toBeChecked();
		await expect(page.getByPlaceholder('Enter destination path (e.g., /path/to/output)')).toBeVisible();

		await page.getByText('Rename in place', { exact: true }).click();
		await expect(page.getByPlaceholder('Enter destination path (e.g., /path/to/output)')).toHaveCount(0);
		await expect(page.getByRole('radio', { name: 'Replace existing' })).toHaveCount(0);
		await expect(page.getByText('Rename videos and eligible dedicated folders in place', { exact: true })).toBeVisible();

		const renameOnly = page.getByRole('radio', { name: /Rename video file only/ });
		await page.getByText('Rename video file only', { exact: true }).click();
		await expect(renameOnly).toBeChecked();
		await expect(page.getByText('Rename videos without renaming their containing folders.', { exact: true })).toBeVisible();
		renameOnly.focus();
		await renameOnly.press('ArrowRight');
		await expect(page.getByRole('radio', { name: /Leave video files in place/ })).toBeChecked();
		await expect(page.getByRole('radio', { name: 'Replace existing' })).toBeVisible();
		await expect(page.getByText('Existing metadata merge')).toBeVisible();
		await page.getByText('Replace existing', { exact: true }).click();
		await expect(page.getByText('Replace existing enabled media (destructive)', { exact: true })).toBeVisible();
		await expect(page.getByRole('heading', { name: 'This operation will…' })).toBeVisible();
	});

	test('preserves the plan through Manual and keeps compact confirmation usable at 320px', async ({ page }) => {
		await page.setViewportSize({ width: 320, height: 700 });
		await page.getByText('Leave video files in place', { exact: true }).click();
		const replace = page.getByRole('radio', { name: 'Replace existing' });
		await page.getByText('Replace existing', { exact: true }).click();
		await expect(replace).toBeChecked();
		await page.getByRole('button', { name: /IPX-123\.mp4/ }).click();
const optionsButton = page.getByRole('button', { name: /^Options$/ });
		await optionsButton.click();
		await expect(optionsButton).toHaveAttribute('aria-expanded', 'true');
		const optionsPanel = page.getByRole('region', { name: 'Options' });
		await expect(optionsPanel).toBeVisible();
		const menuGeometry = await page.evaluate(() => {
			const panel = document.getElementById('browse-options-panel')!;
			const bar = document.getElementById('browse-options-trigger')!.closest('.sticky')!;
			const panelRect = panel.getBoundingClientRect();
			const barRect = bar.getBoundingClientRect();
			return { panelLeft: panelRect.left, panelRight: panelRect.right, panelTop: panelRect.top, barHeight: barRect.height, viewportWidth: innerWidth };
		});
		expect(menuGeometry.panelLeft).toBeGreaterThanOrEqual(0);
		expect(menuGeometry.panelRight).toBeLessThanOrEqual(menuGeometry.viewportWidth);
		expect(menuGeometry.panelTop).toBeGreaterThanOrEqual(0);
		expect(menuGeometry.barHeight).toBeLessThan(200);
		const manualMode = page.getByRole('checkbox', { name: /Provide IDs or URLs manually/ });
		await expect(manualMode).toBeVisible();
		await manualMode.check();
		await expect(manualMode).toBeChecked();

		await expect(page.getByTestId('compact-plan-summary')).toHaveCount(0);
		await expect(page.getByRole('button', { name: /Continue to manual review/i })).toBeVisible();
		for (const width of [320, 375, 768, 1024]) {
			await page.setViewportSize({ width, height: 700 });
			const layout = await page.evaluate(() => ({ innerWidth: window.innerWidth, scrollWidth: document.documentElement.scrollWidth }));
			expect(layout.scrollWidth, `overflow at ${width}px`).toBeLessThanOrEqual(layout.innerWidth);
		}
		await page.setViewportSize({ width: 320, height: 700 });

		await page.getByRole('button', { name: /Continue to manual review/i }).click();
		await page.waitForURL('**/manual');
		await expect(page.getByRole('heading', { name: 'Manual Scrape' })).toBeVisible();
		await expect(page.getByText('Leave video files in place')).toBeVisible();
		await expect(page.getByText('Replace existing enabled media (destructive)', { exact: true })).toBeVisible();

		const scrapeRequest = page.waitForRequest((request) => request.url().includes('/api/v1/batch/scrape') && request.method() === 'POST');
		await page.getByRole('button', { name: /Start manual scrape/i }).click();
		const submitted = await scrapeRequest;
		const body = submitted.postDataJSON();
		expect(body.apply_plan).toMatchObject({ video_operation: 'leave-in-place', media_policy: 'replace' });
	});
});

test('Review hydrates replacement and shows the non-revertible effective-plan warning', async ({ page }) => {
	await setupMockRoutes(page);
	const job = {
		id: 'plan-job', status: 'completed', total_files: 1, completed: 1, failed: 0,
		operation_count: 0, reverted_count: 0, excluded: {}, progress: 100, destination: '/stale',
		started_at: '2026-01-01T00:00:00Z', update: true,
		apply_plan: { version: 1, video_operation: 'leave-in-place', nfo_output: 'write', media_policy: 'replace', merge: { scalar_strategy: 'prefer-nfo', array_strategy: 'merge' } },
		results: { '/videos/IPX-123.mp4': { result_id: 'result-1', file_path: '/videos/IPX-123.mp4', movie_id: 'IPX-123', status: 'completed', started_at: '2026-01-01T00:00:00Z', is_multi_part: false, part_number: 0, part_suffix: '', movie: { id: 'IPX-123', title: 'Plan Movie' } } }
	};
	await page.route('**/api/v1/batch/plan-job**', async (route) => {
		if (route.request().url().includes('/preview')) {
			return route.fulfill({ json: { operation_mode: 'metadata-artwork', source_path: '/videos/IPX-123.mp4', video_files: ['/videos/IPX-123.mp4'], effective_apply: { plan: job.apply_plan, merge_override: 'none' } } });
		}
		return route.fulfill({ json: job });
	});

	await page.goto('/review/plan-job');
	await page.waitForLoadState('networkidle');
	await page.getByRole('button', { name: 'Options' }).click();
	await expect(page.getByRole('checkbox', { name: /Replace Existing Media/i })).toBeChecked();
	await page.getByRole('button', { name: 'Detail', exact: true }).click();
	await page.getByRole('button', { name: /Preview output/i }).click();
	await expect(page.getByRole('dialog', { name: 'Output Preview' })).toBeVisible();
	await expect(page.getByText(/Replacing existing media is destructive and is not covered by organize rollback/)).toBeVisible();
});