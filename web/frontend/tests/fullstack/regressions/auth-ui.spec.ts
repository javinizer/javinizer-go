/**
 * Auth UI full-stack spec.
 *
 * Pins the authentication UI flow end-to-end against the real e2emock
 * backend: the login screen render (when unauthenticated), the login
 * form submit (real POST /api/v1/auth/login), the post-login redirect to
 * the app shell (Navigation with the username + Logout button), the
 * logout button click (real POST /api/v1/auth/logout), + the post-logout
 * return to the login screen.
 *
 * Why this matters: the existing auth.spec.ts covers the auth API
 * contract (401 without cookie, login success/failure, session cookie)
 * via the `request` fixture — it never renders the browser. The login
 * screen is the entry point to the entire app, + the +layout.svelte auth
 * guard (authLoading → authUnavailable → login/setup → authenticated) is
 * the most user-facing auth logic. A regression in the layout's
 * refreshAuthStatus wiring, the login form binding, the post-login
 * authAuthenticated state flip, or the Navigation logout button is
 * invisible to the API-only spec.
 *
 * Technique: the fullstack config's chromium project auto-authenticates
 * every test via storageState (the global-setup's saved cookie). To test
 * the unauthenticated login screen, these tests override storageState to
 * undefined — Playwright creates a fresh browser context with NO cookies,
 * so the layout's refreshAuthStatus finds no session + renders the login
 * screen. This is the documented Playwright pattern for per-test auth
 * overrides.
 *
 * Note on the setup screen: the e2e backend is pre-bootstrapped
 * (initialized=true) by cmd/javinizer-e2e, so the "First-Time Setup"
 * screen is not reachable without tearing down the backend's auth state.
 * This spec covers the login screen (the realistic daily-use path) + the
 * logout flow. The setup screen is left to the API-level coverage.
 */
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { BACKEND_BASE } from '../helpers';

// Override the project-level storageState for these tests — start with a
// clean (unauthenticated) browser context so the layout renders the login
// screen instead of auto-redirecting to the authenticated app. Passing an
// explicit empty object (not undefined) is required: undefined is treated
// as "not set" + falls back to the project's saved auth-state.json.
test.use({ storageState: { cookies: [], origins: [] } });

const LOGIN_HEADING = 'Login Required';
const DASHBOARD_HEADING = 'Javinizer Control Center';
const USERNAME = 'admin';
const PASSWORD = 'adminpassword123';

async function gotoRootAndWaitForHydration(page: Page): Promise<void> {
	await page.goto('/');
	await page.waitForLoadState('networkidle');
}

test.describe('Auth UI: login + logout against the real e2emock backend', () => {
	test('unauthenticated visit renders the login screen (not the app)', async ({
		page,
	}: {
		page: Page;
	}) => {
		// Navigate to the root. The layout's onMount calls refreshAuthStatus
		// → GET /api/v1/auth/status. With no cookie, authenticated=false +
		// initialized=true → the "Login Required" screen renders.
		await gotoRootAndWaitForHydration(page);
		await expect(page.getByRole('heading', { name: LOGIN_HEADING })).toBeVisible({
			timeout: 15_000,
		});

		// The login form renders with username + password fields + the
		// "Sign In" button. Asserting these pins the form mounted.
		await expect(page.locator('#login-username')).toBeVisible({ timeout: 10_000 });
		await expect(page.locator('#login-password')).toBeVisible();
		await expect(page.getByRole('button', { name: /^Sign In$/ })).toBeVisible();

		// The app shell (Navigation + dashboard) does NOT render.
		await expect(page.getByRole('heading', { name: DASHBOARD_HEADING })).toHaveCount(0);
	});

	test('login with valid credentials → app shell renders + logout button shows the username', async ({
		page,
	}: {
		page: Page;
	}) => {
		await gotoRootAndWaitForHydration(page);
		await expect(page.getByRole('heading', { name: LOGIN_HEADING })).toBeVisible({
			timeout: 15_000,
		});

		// Fill + submit the login form. handleLoginSubmit calls
		// apiClient.loginAuth (real POST /api/v1/auth/login) → on success,
		// refreshAuthStatus flips authAuthenticated=true → the layout
		// re-renders the authenticated app shell (Navigation + page).
		await page.locator('#login-username').fill(USERNAME);
		await page.locator('#login-password').fill(PASSWORD);
		await page.getByRole('button', { name: /^Sign In$/ }).click();

		// The dashboard renders (the root route is the control center).
		// This pins the authAuthenticated state flipped + the layout
		// transitioned from the login screen to the app shell.
		await expect(page.getByRole('heading', { name: DASHBOARD_HEADING })).toBeVisible({
			timeout: 15_000,
		});

		// The Navigation's logout button renders with the username. The
		// button text is "{username} · Logout" (md+ screens) or "Logout"
		// (narrow). Assert the combined text to pin the username propagated
		// from refreshAuthStatus → authUsername → Navigation.
		await expect(page.getByTitle('Logout')).toBeVisible({ timeout: 10_000 });
		await expect(page.getByTitle('Logout')).toContainText(USERNAME);
	});

	test('login with WRONG password → error renders + app shell does NOT', async ({
		page,
	}: {
		page: Page;
	}) => {
		await gotoRootAndWaitForHydration(page);
		await expect(page.getByRole('heading', { name: LOGIN_HEADING })).toBeVisible({
			timeout: 15_000,
		});

		await page.locator('#login-username').fill(USERNAME);
		await page.locator('#login-password').fill('wrong-password');
		await page.getByRole('button', { name: /^Sign In$/ }).click();

		// handleLoginSubmit's catch sets authError → the error banner renders
		// (a div with text-destructive). The login screen stays (not the app).
		// Assert the error banner is visible + the dashboard is not. The
		// banner's class is border-destructive/40 (Tailwind opacity modifier),
		// so match via the text-destructive class which is stable.
		await expect(page.locator('div.text-destructive').first()).toBeVisible({
			timeout: 10_000,
		});
		await expect(page.getByRole('heading', { name: DASHBOARD_HEADING })).toHaveCount(0);
		// The login heading is still visible (still on the login screen).
		await expect(page.getByRole('heading', { name: LOGIN_HEADING })).toBeVisible();
	});

	test('logout button → POST /auth/logout → returns to the login screen', async ({
		page,
		request,
	}: {
		page: Page;
		request: APIRequestContext;
	}) => {
		// ── 1. Log in via the UI (reuse the valid-login flow) ────────────
		await gotoRootAndWaitForHydration(page);
		await expect(page.getByRole('heading', { name: LOGIN_HEADING })).toBeVisible({
			timeout: 15_000,
		});
		await page.locator('#login-username').fill(USERNAME);
		await page.locator('#login-password').fill(PASSWORD);
		await page.getByRole('button', { name: /^Sign In$/ }).click();
		await expect(page.getByRole('heading', { name: DASHBOARD_HEADING })).toBeVisible({
			timeout: 15_000,
		});

		// ── 2. Click the Logout button ──────────────────────────────────
		// handleLogout calls apiClient.logoutAuth (real POST /api/v1/auth/
		// logout) → on completion, authAuthenticated=false → the layout
		// re-renders the login screen.
		await page.getByTitle('Logout').click();

		// The login screen re-renders (auth guard flipped back).
		await expect(page.getByRole('heading', { name: LOGIN_HEADING })).toBeVisible({
			timeout: 15_000,
		});
		await expect(page.getByRole('heading', { name: DASHBOARD_HEADING })).toHaveCount(0);

		// ── 3. Cross-check: the session cookie is now invalid ───────────
		// After logout, an authenticated API call with the (now-invalid)
		// cookie should be rejected. This pins the logout actually hit the
		// backend (not just a client-side state flip). We can't read the
		// browser cookie easily here, so assert via the page's auth status
		// fetch: navigating again still shows the login screen (cookie dead).
		await page.goto('/browse');
		await expect(page.getByRole('heading', { name: LOGIN_HEADING })).toBeVisible({
			timeout: 15_000,
		});
	});
});
