import { test, expect, type Page } from '@playwright/test';

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
		if (url.pathname === '/api/v1/browse') return route.fulfill({ json: { current_path: '/videos', parent_path: '/', items: [{ name: 'child-folder', path: '/videos/child-folder', is_dir: true, size: 0, mod_time: '2026-01-01T00:00:00Z' }, { name: 'IPX-123.mp4', path: '/videos/IPX-123.mp4', is_dir: false, size: 10, mod_time: '2026-01-01T00:00:00Z' }] } });
		if (url.pathname.startsWith('/api/v1/version')) return route.fulfill({ json: { current: 'test', update_available: false, source: 'disabled' } });
		return route.fulfill({ json: {} });
	});
}

test('filters browse entries without losing navigation or hidden selections', async ({ page }) => {
 await setupBrowseRoutes(page);
 await page.goto('/browse');
 const type = page.getByRole('combobox', { name: 'Show', exact: true });
 await expect(page.getByText('child-folder', { exact: true })).toBeVisible();
 await type.selectOption('files');
 await expect(page.getByText('child-folder', { exact: true })).toHaveCount(0);
 await expect(page.getByText('IPX-123.mp4', { exact: true })).toBeVisible();
 await type.selectOption('folders');
 await expect(page.getByText('IPX-123.mp4', { exact: true })).toHaveCount(0);
 await expect(page.getByText('child-folder', { exact: true })).toBeVisible();
 await type.selectOption('all');
 await expect(page.getByText('IPX-123.mp4', { exact: true })).toBeVisible();
 await page.setViewportSize({ width: 375, height: 700 });
 await expect(type).toBeVisible();
});
