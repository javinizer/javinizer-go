/**
 * Auth regression spec.
 *
 * Real stack: browser → Vite proxy → real /api/v1/auth/* on the Go binary.
 * The endpoint is registered on the real router
 * (auth.RegisterPublicRoutes — login + status are public; everything else
 * under /api/v1/* is behind auth.RequireTokenOrSession middleware).
 *
 * Pins:
 * - Unauthenticated write attempts to a protected endpoint get 401 (the
 *   middleware fires — a regression that exposes /api/v1/batch/scrape
 *   without auth would silently let anyone scrape).
 * - The login endpoint accepts the JAVINIZER_E2E_AUTH-bootstrapped
 *   admin/adminpassword123 credentials and returns authenticated=true.
 * - /api/v1/auth/status reports initialized=true without auth (so the
 *   frontend's setup-vs-login gate works).
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';
import { DEFAULT_INPUT_DIR } from '../helpers/fixtures';

test.describe('Auth: real auth manager + middleware enforce auth on protected endpoints', () => {
	test('POST /api/v1/batch/scrape without a session cookie returns 401 + authentication required', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Use a fresh request context that does NOT inherit the global-setup
		// storageState — playwright's `request` fixture inherits storageState
		// from the test config, so we explicitly bypass it by issuing the
		// request with the default (no-cookies) storageState via
		// request.newContext(). Simplest portable path: use request.post
		// after explicitly clearing cookies via a manual context.
		//
		// Easier: use the browser's `page` context which we can clear, OR
		// equivalently assert that the response code is 401 by issuing
		// via a new APIRequestContext without storageState. Playwright's
		// `request` fixture in the chromium project DOES carry the
		// storageState cookie — so for the unauthenticated path we use
		// browser.newContext() explicitly.
		//
		// Implementation: issue the POST via the page's `fetch` (browser
		// context, no cookies set) — equivalent to a third-party caller.
		const resp = await request.post(`${BACKEND_BASE}/api/v1/batch/scrape`, {
			data: { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`], selected_scrapers: ['e2emock'] },
			failOnStatusCode: false,
			// Override storageState for THIS call only — pass an empty
			// cookie jar by using a header that strips any session cookie.
			headers: { Cookie: '' },
		});

		// [bug class: auth middleware bypass] If a regression to
		// auth.RequireTokenOrSession made it optional, this would 200 +
		// create a job. Pin the 401.
		expect(resp.status(), 'unauthenticated scrape must return 401, not silently succeed').toBe(401);
		const body = await resp.json();
		expect(body.error, 'error message must be the canonical "authentication required"').toBe(
			'authentication required',
		);
	});

	test('GET /api/v1/auth/status (unauthenticated) reports the bootstrapped admin user is initialized', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// /auth/status is intentionally public — the frontend's setup-vs-login
		// gate reads this before knowing whether to render /setup or /login.
		// The e2e backend auto-initializes admin via JAVINIZER_E2E_AUTH=true,
		// so initialized=true + authenticated=false (no cookie yet).
		const resp = await request.get(`${BACKEND_BASE}/api/v1/auth/status`, {
			headers: { Cookie: '' }, // bypass storageState
		});
		expect(resp.ok()).toBeTruthy();
		const body = await resp.json();
		expect(body.initialized, 'e2e backend must auto-initialize the admin user').toBeTruthy();
		expect(
			body.authenticated,
			'unauthenticated /auth/status must report authenticated=false',
		).toBeFalsy();
	});

	test('POST /api/v1/auth/login with admin/adminpassword123 returns authenticated=true + sets a session cookie', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Login via the real auth manager — exercises the real SQLite
		// session table write + the Argon2id password verification path.
		const resp = await request.post(`${BACKEND_BASE}/api/v1/auth/login`, {
			data: { username: 'admin', password: 'adminpassword123' },
			headers: { Cookie: '' }, // bypass any inherited storageState for a clean login
			failOnStatusCode: false,
		});
		expect(
			resp.ok(),
			`login must succeed with valid credentials, got ${resp.status()}`,
		).toBeTruthy();
		const body = await resp.json();
		expect(body.authenticated, 'login response must confirm authenticated=true').toBeTruthy();
		expect(body.username, 'login response must echo the username').toBe('admin');

		// Session cookie must be present in the response — the global-setup
		// path scrapes + injects it. Here we just assert it's set.
		const setCookie = resp.headers()['set-cookie'] ?? '';
		expect(setCookie, 'login must set a session cookie via Set-Cookie header').toBeTruthy();
		expect(setCookie.toLowerCase(), 'session cookie must be HttpOnly (XSS protection)').toContain(
			'httponly',
		);
	});

	test('POST /api/v1/auth/login with WRONG password does not authenticate', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Wrong-password path exercises the real auth manager's
		// constant-time password compare + rate-limit counter. With
		// SetDisableRateLimit(true) on the e2e backend, no lockout — just
		// a non-authenticated response.
		const resp = await request.post(`${BACKEND_BASE}/api/v1/auth/login`, {
			data: { username: 'admin', password: 'wrong-password' },
			headers: { Cookie: '' },
			failOnStatusCode: false,
		});
		// The login endpoint returns 401 on bad credentials — not 200 with
		// authenticated=false — to match the canonical "wrong password" UI
		// flow the frontend handles.
		expect(resp.status(), 'wrong password must not return 2xx').toBe(401);

		const body = await resp.json().catch(() => ({}));
		expect(body.authenticated, 'wrong-password login must not report authenticated=true').not.toBe(
			true,
		);
	});

	test('authenticated POST /api/v1/batch/scrape (using storageState cookie) succeeds with 200 + job_id', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Sanity check the inverse of the first test: WITH the global-setup
		// session cookie, the same protected endpoint accepts the request.
		// This proves the 401 in the first test was really about cookies +
		// not about a routing / method error.
		await loginAgainstRealBackend(request); // idempotent if storageState already has it

		const resp = await request.post(`${BACKEND_BASE}/api/v1/batch/scrape`, {
			data: { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`], selected_scrapers: ['e2emock'] },
			failOnStatusCode: false,
		});
		expect(resp.status(), 'authenticated scrape must return 200').toBe(200);
		const body = await resp.json();
		expect(body.job_id, 'authenticated scrape must return a job_id').toBeTruthy();
	});
});
