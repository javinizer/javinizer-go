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
 *   self-upgrade button (NOT the "View release" link), and renders NO command
 *   rows (the button IS the upgrade — no terminal action exists).
 * - Nav UpdateIndicator popover (docker env): renders the upgrade guidance
 *   as discrete, labeled, per-row-copyable commands (docker pull / compose)
 *   — the API's structured upgrade_commands replace the old prose "sh" blob,
 *   and copying a row yields exactly its command.
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
		await expect(
			popover.locator('a[href*="github.com/javinizer/javinizer-go/releases"]'),
		).toHaveCount(0);

		// Desktop env: no command rows / no prose block — the button IS the
		// upgrade. (The guard: install_environment !== 'desktop', and the API
		// returns no upgrade_commands for desktop.)
		await expect(popover.locator('[data-upgrade-commands]')).toHaveCount(0);
		await expect(popover.locator('pre')).toHaveCount(0);

		// The env badge still labels the install type.
		await expect(popover).toContainText('Desktop app');
	});

	test('nav popover overlays page content (not trapped inside the nav scroll container)', async ({
		page,
	}: {
		page: Page;
	}) => {
		await page.goto('/jobs');
		await page.waitForLoadState('domcontentloaded');

		const indicatorButton = page.locator('button[aria-label="Update available"]');
		await expect(indicatorButton).toBeVisible({ timeout: 10_000 });
		await indicatorButton.click();

		const popover = page.locator('[role="dialog"][aria-label="Update details"]');
		await expect(popover).toBeVisible();

		// Regression pin: the UpdateIndicator slot must NOT live inside the
		// nav's overflow-x-auto scroll container. A non-visible overflow-x
		// makes overflow-y compute to `auto` (CSS Overflow 3), so an absolute
		// popover inside that container is clipped into the nav's scroll box
		// and paints "inline" within the navbar (with a scrollbar) instead of
		// dropping down as an overlay over the page.
		//
		// (a) Hit-test the popover's center: a clipped box still reports a
		//     bounding rect, so toBeVisible alone can't catch this — the paint
		//     order must actually reach the popover.
		const { hitInside, popoverBottom, navBottom } = await page.evaluate(() => {
			const nav = document.querySelector('nav');
			if (!nav) throw new Error('nav not found');
			const pop = document.querySelector('[role="dialog"][aria-label="Update details"]');
			if (!pop) throw new Error('popover not found');
			const nb = nav.getBoundingClientRect();
			const pb = pop.getBoundingClientRect();
			const hit = document.elementFromPoint(pb.left + pb.width / 2, pb.top + pb.height / 2);
			return {
				hitInside: hit ? pop.contains(hit) : false,
				popoverBottom: pb.bottom,
				navBottom: nb.bottom,
			};
		});
		expect(popoverBottom, 'popover should extend below the 64px nav row').toBeGreaterThan(
			navBottom,
		);
		expect(
			hitInside,
			'popover center must be hit-testable (not clipped by an overflow ancestor)',
		).toBe(true);

		// (b) A trapped popover inflates its scroll container's scrollable
		//     area, growing a vertical scrollbar inside the 64px navbar (the
		//     visible symptom of the broken "inline" rendering). Only flag
		//     elements whose computed overflow-y actually CLIPS (auto/scroll/
		//     hidden/clip) — scrollHeight also exceeds clientHeight for
		//     overflow:visible ancestors of an absolutely positioned popover,
		//     which is normal and clips nothing. (A resolved auto/scroll/hidden
		//     overflow-x computes overflow-y to auto, so checking the computed
		//     overflow-y covers that scroll-container case too.)
		const anyNavChildClips = await page.evaluate(() => {
			const nav = document.querySelector('nav');
			if (!nav) throw new Error('nav not found');
			const clipping = new Set(['auto', 'scroll', 'hidden', 'clip']);
			return Array.from(nav.querySelectorAll<HTMLElement>('*')).some((el) => {
				const { overflowY } = getComputedStyle(el);
				return (
					clipping.has(overflowY) && el.clientHeight > 0 && el.scrollHeight > el.clientHeight + 1
				);
			});
		});
		expect(
			anyNavChildClips,
			'no element inside the nav should vertically clip when the popover opens',
		).toBe(false);
	});

	test('nav popover (docker env) shows pull + compose rows with per-row copy', async ({
		page,
		context,
	}: {
		page: Page;
		context: import('@playwright/test').BrowserContext;
	}) => {
		// Docker install: no in-app upgrade possible (read-only image). The
		// panel must offer the terminal commands as discrete, copyable rows
		// instead of a prose "shell snippet".
		await page.unroute('**/api/v1/version');
		await page.route('**/api/v1/version', async (route) => {
			if (route.request().url().includes('/version/check')) {
				await route.continue();
				return;
			}
			await route.fulfill({
				status: 200,
				json: {
					current: 'v1.0.0',
					latest: 'v1.2.0',
					update_available: true,
					prerelease: false,
					checked_at: '2026-07-07T01:00:00Z',
					source: 'cached',
					install_environment: 'docker',
					upgrade_instructions:
						'Running in Docker. Pull the latest image and recreate the container: ' +
						'docker pull ghcr.io/javinizer/javinizer-go:latest',
					upgrade_commands: [
						{ key: 'docker_pull', command: 'docker pull ghcr.io/javinizer/javinizer-go:latest' },
						{ key: 'docker_compose', command: 'docker compose pull && docker compose up -d' },
					],
				},
			});
		});

		await page.goto('/jobs');
		await page.waitForLoadState('domcontentloaded');

		const indicatorButton = page.locator('button[aria-label="Update available"]');
		await expect(indicatorButton).toBeVisible({ timeout: 10_000 });
		await indicatorButton.click();

		const popover = page.locator('[role="dialog"][aria-label="Update details"]');
		await expect(popover).toBeVisible();

		// Environment-aware lead-in + one row per command, labeled.
		await expect(popover).toContainText('Pull the new image, then recreate the container');
		const pullRow = popover.locator('[data-upgrade-command="docker_pull"]');
		const composeRow = popover.locator('[data-upgrade-command="docker_compose"]');
		await expect(pullRow).toContainText('docker pull ghcr.io/javinizer/javinizer-go:latest');
		await expect(pullRow).toContainText('Docker');
		await expect(composeRow).toContainText('docker compose pull && docker compose up -d');
		await expect(composeRow).toContainText('Compose');

		// Docker env: no in-app "Update & restart" button (image is read-only),
		// and no fake sh/prose block.
		await expect(popover.getByRole('button', { name: /update.*restart/i })).toHaveCount(0);
		await expect(popover.locator('pre')).toHaveCount(0);

		// Clicking a row's copy button puts exactly that command on the
		// clipboard — not the surrounding guidance prose.
		await context.grantPermissions(['clipboard-read', 'clipboard-write']);
		await composeRow.getByRole('button', { name: /copy/i }).click();
		const clipboard = await page.evaluate(() => navigator.clipboard.readText());
		expect(clipboard).toBe('docker compose pull && docker compose up -d');
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
		await expect(
			versionBlock.locator('a[href*="github.com/javinizer/javinizer-go/releases"]'),
		).toHaveCount(0);
	});
});
