/**
 * r18.dev dump manual upload (settings) — mocked API, no backend.
 *
 * Pins the manual-upload UX contract end-to-end through the real SvelteKit
 * frontend:
 * - The settings section exposes an "Upload Dump" picker in the absent state.
 * - Selecting a file posts multipart/form-data (browser-generated boundary)
 *   to /api/v1/r18dev/dump/upload.
 * - After the staged 202, the section follows the async job via status
 *   polling and lands on the same "dump present" state as a download, with
 *   the uploaded filename's provenance and date shown (raw .sql.gz path).
 * - A synchronous 400 renders the server-provided message verbatim.
 *
 * Run via the frontend-only suite: make test-e2e-frontend
 */
import { test, expect, type Page } from '@playwright/test';

async function mockAuth(page: Page) {
	await page.route('**/api/v1/auth/status', (route) =>
		route.fulfill({
			status: 200,
			json: { initialized: true, authenticated: true, username: 'admin' },
		}),
	);
}

const ABSENT = {
	present: false,
	running: false,
	path: '/data/r18dev/r18dev_dump.db',
	enabled: true,
};

const PRESENT = {
	present: true,
	running: false,
	row_count: 1,
	path: '/data/r18dev/r18dev_dump.db',
	size_bytes: 123456,
	source_url: 'r18dotdev_dump_2026-09-20.sql.gz',
	source_date: '2026-09-20',
	imported_at: '2026-09-27T00:00:00Z',
	enabled: true,
};

async function mockConfig(page: Page) {
	await page.route('**/api/v1/config**', (route) => {
		if (route.request().method() === 'GET') {
			return route.fulfill({
				status: 200,
				json: { metadata: { r18dev_dump: { enabled: true } } },
			});
		}
		return route.fulfill({ status: 200, json: {} });
	});
}

test.describe('r18dev dump manual upload', () => {
	test('upload happy path: multipart post, then present status with filename provenance', async ({
		page,
	}) => {
		await mockAuth(page);
		await mockConfig(page);
		let uploaded = false;
		await page.route('**/api/v1/r18dev/dump/upload', async (route) => {
			expect(route.request().method()).toBe('POST');
			const ct = route.request().headers()['content-type'] ?? '';
			expect(ct).toMatch(/^multipart\/form-data; boundary=/);
			uploaded = true;
			await route.fulfill({ status: 202, json: { message: 'upload staged' } });
		});
		await page.route('**/api/v1/r18dev/dump/status', async (route) => {
			await route.fulfill({ status: 200, json: uploaded ? PRESENT : ABSENT });
		});

		await page.goto('/settings');
		await page.waitForLoadState('domcontentloaded');

		// Expand the r18.dev Dump section (collapsed by default).
		const sectionToggle = page.getByRole('button', { name: /r18\.dev Dump/i }).first();
		await expect(sectionToggle).toBeVisible({ timeout: 15_000 });
		await sectionToggle.click();

		const fileInput = page.locator('input[type="file"]');
		await expect(fileInput).toHaveCount(1);

		const file = {
			name: 'r18dotdev_dump_2026-09-20.sql.gz',
			mimeType: 'application/gzip',
			buffer: Buffer.from('pretend-gzip'),
		};
		await fileInput.setInputFiles(file);

		// The job completes via status polling: present state shows rows,
		// source date, and the update/clear controls.
		await expect(page.getByText('Dump is present and ready')).toBeVisible({ timeout: 15_000 });
		await expect(page.getByText('2026-09-20')).toBeVisible();
		expect(uploaded).toBe(true);
	});

	test('synchronous 400 renders the server message verbatim', async ({ page }) => {
		await mockAuth(page);
		await mockConfig(page);
		await page.route('**/api/v1/r18dev/dump/upload', async (route) => {
			await route.fulfill({ status: 400, json: { error: 'multipart: no file part' } });
		});
		await page.route('**/api/v1/r18dev/dump/status', async (route) => {
			await route.fulfill({ status: 200, json: ABSENT });
		});

		await page.goto('/settings');
		await page.waitForLoadState('domcontentloaded');

		const sectionToggle = page.getByRole('button', { name: /r18\.dev Dump/i }).first();
		await expect(sectionToggle).toBeVisible({ timeout: 15_000 });
		await sectionToggle.click();

		await page.locator('input[type="file"]').setInputFiles({
			name: 'junk.bin',
			mimeType: 'application/octet-stream',
			buffer: Buffer.from('x'),
		});

		await expect(page.getByText('multipart: no file part')).toBeVisible({ timeout: 10_000 });
	});
});
