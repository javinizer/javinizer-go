/**
 * Full-stack global setup: authenticate against the REAL running backend
 * (spawned via Playwright's webServer config) and persist the session
 * cookie for the test specs to reuse.
 *
 * Drives the REAL /api/v1/auth/login endpoint served by cmd/javinizer-e2e.
 * The Go binary auto-initializes the admin user via JAVINIZER_E2E_AUTH=true
 * so Playwright logs in with admin/adminpassword123 against the real auth
 * manager + real SQLite session table.
 *
 * Why APIRequestContext login + manual cookie injection (instead of the
 * tests/e2e/global-setup.ts pattern of filling the login form via DOM):
 * the real SvelteKit frontend's auth gate diverges from the mocked-API
 * flow once the backend pre-initializes the admin user — debugging showed
 * the form-fill path unreliable on the live backend, while the API
 * endpoint consistently returns 200 + Set-Cookie. We authenticate via the
 * API directly + scrape the Set-Cookie header + inject into the browser
 * context's cookie jar.
 *
 * The persisted storageState carries the cookie for BOTH the per-test
 * `page` fixture (browser context inherits via storageState reuse) AND
 * the per-test `request` fixture (Playwright wires APIRequestContext's
 * cookies from storageState too).
 */
import { mkdirSync } from 'node:fs';
import { writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

import { test as setupTest, expect } from '@playwright/test';

import { ensureFixtureDirs } from './helpers/fixtures';

const STORAGE_DIR = dirname(fileURLToPath(import.meta.url));
const STORAGE_PATH = resolve(STORAGE_DIR, '.auth', 'auth-state.json');

setupTest(
	'authenticate against real backend and save storage state',
	async ({ browser, baseURL, request }) => {
		// Seed both fixture dirs (input files written here; output dir used as
		// the organize-preview / organize-apply destination).
		await ensureFixtureDirs();

		// Health probe — Playwright's webServer config already curls /health,
		// belt + suspenders.
		for (let i = 0; i < 40; i++) {
			try {
				if ((await request.get('/health')).ok()) break;
			} catch {
				/* retry */
			}
			await new Promise((resolve) => setTimeout(resolve, 500));
		}

		// Verify auth is initialized — JAVINIZER_E2E_AUTH=true triggers
		// BootstrapAPI auto-setup of the admin user.
		const statusResp = await request.get('/api/v1/auth/status');
		expect(statusResp.ok()).toBeTruthy();
		const statusJson = await statusResp.json();
		expect(statusJson.initialized, 'e2e backend must auto-initialize auth user').toBeTruthy();

		// Login via real API — capture the Set-Cookie header. Playwright surfaces
		// response headers via response.headers()['set-cookie'].
		const loginResp = await request.post('/api/v1/auth/login', {
			data: { username: 'admin', password: 'adminpassword123' },
			maxRedirects: 0,
		});
		expect(
			loginResp.ok(),
			`login against real backend failed: ${loginResp.status()} ${await loginResp.text()}`,
		).toBeTruthy();
		const loginJson = await loginResp.json();
		expect(loginJson.authenticated).toBeTruthy();

		const setCookieHeader = loginResp.headers()['set-cookie'] ?? '';
		const cookies = parseSetCookieHeader(
			setCookieHeader,
			new URL(baseURL ?? 'http://localhost:5175'),
		);

		if (cookies.length === 0) {
			throw new Error(
				`Expected Set-Cookie on login response but got empty header — backend may not be returning a session cookie. Headers: ${JSON.stringify(loginResp.headers())}`,
			);
		}

		// Open a fresh browser context + manually inject the session cookie so
		// the per-test `page` fixture inherits a logged-in state via
		// context.storageState(). The base URL is the Vite dev server
		// (5175 by default) — Vite's proxy forwards /api to the backend on
		// 18080, so domain/localhost cookies apply at the Vite level.
		const context = await browser.newContext({ baseURL });
		await context.addCookies(cookies);

		// Sanity check: load the app's root page and verify it doesn't crash.
		const page = await context.newPage();
		await page.goto('');
		await page.waitForLoadState('domcontentloaded');

		mkdirSync(dirname(STORAGE_PATH), { recursive: true });

		const state = await context.storageState();
		await writeFile(STORAGE_PATH, JSON.stringify(state));
		await context.close();
	},
);

/**
 * Parse a Set-Cookie response header into Playwright's Cookie[] shape.
 *
 * Auth manager sets ONE session cookie named "javinizer_session" — we keep
 * this minimal + focused on the single-session case. The cookie binds to
 * the Vite dev server's host so the browser sends it on every proxied
 * /api + /ws + /health request.
 */
function parseSetCookieHeader(
	header: string,
	url: URL,
): Array<{ name: string; value: string; domain: string; path: string }> {
	if (!header) return [];

	// Only the first segment before the first ";" is the name=value pair;
	// the rest are attributes (Path, HttpOnly, SameSite, Max-Age, etc.)
	const first = header.split(';')[0]?.trim() ?? '';
	const eq = first.indexOf('=');
	if (eq <= 0) return [];

	const name = first.slice(0, eq).trim();
	const value = first.slice(eq + 1).trim();
	if (!name) return [];

	// Domain = dev server's hostname so the cookie applies to proxied
	// /api + /ws + /health traffic. Path = "/" so every page on the dev
	// server inherits the cookie.
	return [{ name, value, domain: url.hostname, path: '/' }];
}
