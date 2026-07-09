/**
 * Desktop upgrade-CTA + notification spec (mocked install_environment=desktop).
 *
 * Unlike the fullstack suite (real backend, real GitHub, install_environment
 * defaults to "cli"), this spec forces install_environment="desktop" by
 * intercepting /api/v1/version* with page.route. The desktop-specific UI path
 * can't be reached through the real CLI backend (cmd/javinizer-e2e never calls
 * SetInstallEnvironment), so this mocked spec is the only way to e2e-test the
 * in-app self-upgrade surface end-to-end through the real SvelteKit frontend.
 *
 * What this pins:
 * - Nav UpdateIndicator popover (desktop env): renders the "Update & restart"
 *   self-upgrade button (NOT the "View release" link), and HIDES the
 *   backend-provided upgrade_instructions <pre> (the button IS the upgrade —
 *   restating "click the button" + a long GitHub-download fallback is noise).
 * - Settings → Server Settings → "Check for Updates" (desktop env): surfaces
 *   the same "Update & restart" button inline so a user who checks from
 *   Settings can act without hunting for the nav indicator.
 *
 * Run via the frontend-only suite (mocked, no backend):
 *   make test-e2e-frontend
 *   # or: npx playwright test --config=playwright.frontend.config.ts desktop-upgrade
 *
 * Why a mocked spec lives in tests/frontend/ (not tests/fullstack/): the
 * fullstack suite's convention is NO page.route — it exercises the real
 * transport end-to-end, and the real CLI backend can't produce
 * install_environment=desktop. This spec mocks /version + /auth/status so
 * it needs no backend at all.
 */
import { test, expect, type Page } from '@playwright/test';
import { mockScraperInfos } from './review-helpers';

/** Canned desktop version status returned by the mocked /version endpoints. */
const DESKTOP_STATUS = {
	current: 'v1.0.0',
	latest: 'v1.2.0',
	update_available: true,
	prerelease: false,
	checked_at: '2026-07-07T01:00:00Z',
	source: 'cached',
	install_environment: 'desktop',
	// The backend still returns desktop guidance for the CLI `javinizer upgrade`
	// handoff path; the frontend must NOT render it for desktop (button instead).
	upgrade_instructions:
		'Desktop app: click "Update & restart" in the app, or quit the app first, ' +
		'then download the new bundle from https://github.com/javinizer/javinizer-go/releases ' +
		'and replace your existing app.',
} as const;

/** Mock the auth gate so the SPA renders the authenticated app shell
 * (no real backend). The shared +layout onMount calls getAuthStatus() →
 * GET /api/v1/auth/status; replying {authenticated:true} skips the login
 * screen so the nav + pages render. This keeps the spec backend-free. */
async function mockAuth(page: Page) {
	await page.route('**/api/v1/auth/status', (route) =>
		route.fulfill({
			status: 200,
			json: { initialized: true, authenticated: true, username: 'admin' },
		}),
	);
}

/** Intercept GET + POST /api/v1/version* and reply with the desktop status. */
async function mockDesktopVersion(page: Page) {
	await page.route('**/api/v1/version/check', async (route) => {
		await route.fulfill({ status: 200, json: { ...DESKTOP_STATUS, source: 'fresh' } });
	});
	await page.route('**/api/v1/version', async (route) => {
		// Avoid matching the /check variant twice: only fulfill the exact path.
		if (route.request().url().includes('/version/check')) {
			await route.continue();
			return;
		}
		await route.fulfill({ status: 200, json: DESKTOP_STATUS });
	});
}

test.describe('Desktop upgrade CTA + notification (install_environment=desktop)', () => {
	test.beforeEach(async ({ page }) => {
		await mockAuth(page);
		await mockDesktopVersion(page);
	});

	test('nav popover shows "Update & restart" and hides the instructions text', async ({
		page,
	}: {
		page: Page;
	}) => {
		await page.goto('/jobs');
		await page.waitForLoadState('domcontentloaded');

		// Indicator is visible (update_available=true, source != disabled/none).
		const indicatorButton = page.locator('button[aria-label="Update available"]');
		await expect(indicatorButton).toBeVisible({ timeout: 10_000 });
		await indicatorButton.click();

		const popover = page.locator('[role="dialog"][aria-label="Update details"]');
		await expect(popover).toBeVisible();

		// Desktop env: in-app self-upgrade button (NOT the releases link).
		const upgradeButton = popover.getByRole('button', { name: /update.*restart/i });
		await expect(upgradeButton).toBeVisible();
		await expect(popover.locator('a[href*="github.com/javinizer/javinizer-go/releases"]')).toHaveCount(0);

		// Desktop env: the backend-provided instructions <pre> is hidden — the
		// button IS the upgrade. (The guard: install_environment !== 'desktop'.)
		await expect(popover.locator('pre')).toHaveCount(0);

		// The env badge still labels the install type.
		await expect(popover).toContainText('Desktop app');
	});

	test('Settings "Check for Updates" surfaces the "Update & restart" button inline', async ({
		page,
	}: {
		page: Page;
	}) => {
		// The Settings page loads /api/v1/config + /api/v1/scrapers before its
		// sections render. Mock both (no backend) with the fields
		// ServerSettingsSection binds (system.version_check_*, system.temp_dir,
		// server.host/port) so the section expands.
		await page.route('**/api/v1/config', (r) =>
			r.fulfill({
				status: 200,
				json: {
					server: { host: 'localhost', port: 8765 },
					system: {
						version_check_enabled: true,
						version_check_interval_hours: 24,
						temp_dir: 'data/temp',
					},
				},
			}),
		);
		await page.route('**/api/v1/scrapers', (r) =>
			r.fulfill({ status: 200, json: { scrapers: mockScraperInfos } }),
		);

		await page.goto('/settings');
		await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible({
			timeout: 15_000,
		});

		// Expand the Server Settings section.
		const serverHeader = page.getByRole('button', { name: /^Server Settings/ }).first();
		await expect(serverHeader).toBeVisible({ timeout: 15_000 });
		await serverHeader.click();
		await expect(serverHeader).toHaveAttribute('aria-expanded', 'true');

		// Click "Check for Updates" — checkVersion() POSTs /version/check and
		// sets the local versionStatus, which renders the UpgradeAction CTA.
		const checkButton = page.getByRole('button', { name: /check for updates/i }).first();
		await expect(checkButton).toBeVisible({ timeout: 10_000 });
		await checkButton.click();

		// The CTA appears in the same version block as the "Check for Updates"
		// button. Scope to that block rather than the fragile Tailwind class.
		const versionBlock = checkButton.locator('xpath=ancestor::div[contains(@class,"bg-muted")]');
		const upgradeButton = versionBlock.getByRole('button', { name: /update.*restart/i });
		await expect(upgradeButton).toBeVisible({ timeout: 10_000 });

		// Non-desktop CTA must not appear in the desktop env.
		await expect(versionBlock.locator('a[href*="github.com/javinizer/javinizer-go/releases"]')).toHaveCount(0);
	});
});
