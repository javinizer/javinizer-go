/**
 * Update-notification indicator regression spec.
 *
 * Real stack: browser → Vite proxy → real /api/v1/version on the Go binary →
 * real update.Service → real GitHub API (api.github.com/repos/javinizer/
 * javinizer-go). NO page.route mocks — the fullstack suite's convention is
 * to exercise the real transport end-to-end.
 *
 * What this pins:
 * - GET /api/v1/version returns a well-formed VersionStatusResponse through
 *   the real stack (the update checker now points at the Go rewrite, not the
 *   legacy Python repo — a prior bug returned 2.6.4 from javinizer/Javinizer).
 * - The nav renders the UpdateIndicator iff the real API reports
 *   update_available=true (and source is not disabled/none). The indicator is
 *   the user-facing notification surface for new releases.
 * - When visible, the popover shows the real latest + current versions and a
 *   deep-link to the Go repo's release page. The prerelease tag renders when
 *   the latest version is a prerelease (the Go repo currently ships only
 *   prereleases, so this is the expected path).
 *
 * Determinism note: the e2e binary's version is v0.0.0-<commit> (dev build,
 * no ldflags) which is always less than any real release tag, so
 * update_available=true is the expected state when GitHub responds. If
 * GitHub rate-limits (60 req/hr unauthenticated — the startup BackgroundCheck
 * + this spec's force-check are 2 hits per run), the API returns an error
 * state with update_available=false and the spec asserts the hidden path
 * instead. Both paths are valid assertions; the spec never flakes — it tests
 * whichever state the real API actually returned.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';

/** Shape of the /api/v1/version response (mirrors VersionStatusResponse). */
interface VersionStatusResponse {
	current: string;
	latest: string;
	update_available: boolean;
	prerelease: boolean;
	checked_at: string;
	source: string;
	error?: string;
}

/** Force a fresh version check against the real GitHub API and return state.
 *
 * Uses a RELATIVE URL so the request flows through the Vite dev-server proxy
 * (localhost:FRONTEND_PORT → 127.0.0.1:18080) carrying the storageState
 * session cookie scoped to localhost — the same path the browser takes.
 * Direct BACKEND_BASE requests would need a separate loginAgainstRealBackend
 * call because the cookie domain doesn't cover 127.0.0.1. */
async function forceCheck(request: APIRequestContext): Promise<VersionStatusResponse> {
	const resp = await request.post('/api/v1/version/check', {
		failOnStatusCode: false,
	});
	expect(resp.ok(), 'POST /api/v1/version/check should return 200').toBeTruthy();
	return (await resp.json()) as VersionStatusResponse;
}

/** Read the cached version status (no GitHub hit — reads the on-disk cache). */
async function getCachedStatus(request: APIRequestContext): Promise<VersionStatusResponse> {
	const resp = await request.get('/api/v1/version', { failOnStatusCode: false });
	expect(resp.ok(), 'GET /api/v1/version should return 200').toBeTruthy();
	return (await resp.json()) as VersionStatusResponse;
}

test.describe('Update indicator: real update.Service + real GitHub API → nav indicator', () => {
	test('GET /api/v1/version returns a well-formed status through the real stack', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		const status = await getCachedStatus(request);

		// Core fields always present regardless of check outcome.
		expect(typeof status.current).toBe('string');
		expect(status.current.length).toBeGreaterThan(0);
		expect(typeof status.update_available).toBe('boolean');
		expect(typeof status.prerelease).toBe('boolean');
		expect(typeof status.source).toBe('string');

		// The update checker must point at the Go rewrite (javinizer/javinizer-go),
		// NOT the legacy Python repo (javinizer/Javinizer). A prior bug returned
		// 2.6.4 — the Python project's release — as the "latest" version. If the
		// check succeeds (source is fresh/cached, not error), latest must NOT be
		// the Python repo's 2.6.4.
		if (status.source !== 'error' && status.source !== 'disabled' && status.source !== 'none') {
			expect(status.latest, 'latest must not be the legacy Python repo version').not.toBe('2.6.4');
			expect(status.latest.length).toBeGreaterThan(0);
		}
	});

	test('nav indicator reflects the real version status', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		// Force a fresh check so the cache reflects the current GitHub state
		// (the startup BackgroundCheck may have rate-limited). This is one
		// GitHub hit; the GET below reads the resulting cache (no hit).
		const fresh = await forceCheck(request);

		// Navigate to any authenticated page — the nav (with UpdateIndicator)
		// renders in the shared layout.
		await page.goto('/jobs');
		await page.waitForLoadState('domcontentloaded');
		await page.waitForLoadState('networkidle');

		const indicatorButton = page.locator('button[aria-label="Update available"]');

		if (fresh.update_available && fresh.source !== 'disabled' && fresh.source !== 'none') {
			// --- Shown path: update available → indicator visible + interactive ---
			await expect(indicatorButton).toBeVisible();
			await expect(indicatorButton).toHaveAttribute('aria-expanded', 'false');

			// Open the popover.
			await indicatorButton.click();
			await expect(indicatorButton).toHaveAttribute('aria-expanded', 'true');

			// Popover shows the real latest + current versions from the API.
			const popover = page.locator('[role="dialog"][aria-label="Update details"]');
			await expect(popover).toBeVisible();
			await expect(popover).toContainText(fresh.latest);
			await expect(popover).toContainText(fresh.current);

			// Prerelease tag renders iff the latest version is a prerelease.
			// The Go repo currently ships only prereleases (v0.x-alpha), so
			// fresh.prerelease is expected true here.
			if (fresh.prerelease) {
				await expect(popover).toContainText('Prerelease available');
				await expect(popover.locator('span.bg-amber-500\\/15')).toBeVisible();
			} else {
				await expect(popover).toContainText('Update available');
			}

			// "View release" link deep-links to the Go repo's release page.
			const releaseLink = popover.locator(
				`a[href*="github.com/javinizer/javinizer-go/releases/tag/${fresh.latest}"]`,
			);
			await expect(releaseLink).toBeVisible();
			await expect(releaseLink).toHaveAttribute('target', '_blank');
			await expect(releaseLink).toHaveAttribute('rel', 'noopener noreferrer');

			// "Check again" button is present and triggers a force-check.
			const checkAgainButton = popover.getByText('Check again');
			await expect(checkAgainButton).toBeVisible();
		} else {
			// --- Hidden path: no update / disabled / error / rate-limited ---
			// The indicator must NOT render — the nav stays clean for up-to-date
			// or offline users. This is the valid fallback when GitHub rate-limits.
			await expect(indicatorButton).toHaveCount(0);
		}
	});
});
