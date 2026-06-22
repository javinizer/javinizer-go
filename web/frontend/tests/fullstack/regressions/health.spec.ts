/**
 * Health endpoint regression spec.
 *
 * Real stack: browser → Vite proxy → `/health` on the Go binary. The endpoint
 * is registered on the real router (system.RegisterCoreRoutes) and reports
 * the scraper registry contents — so a regression that breaks scraper
 * registration, runner wiring, or the health probe's response shape surfaces
 * here as a failing assertion.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

test.describe('Health: GET /health reports real registry + version metadata', () => {
	test('unauthenticated GET /health returns 200 with status=ok + scrapers list including e2emock', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// /health is intentionally unauthenticated — it's the liveness probe
		// the Docker HEALTHCHECK + the Vite webServer config curl-poll. If
		// this endpoint required auth, the container would be marked unhealthy
		// + Playwright would never consider the backend "started". Pin the
		// unauthenticated contract here.
		const resp = await request.get(`${BACKEND_BASE}/health`);
		expect(resp.ok(), `GET /health must return 2xx, got ${resp.status()}`).toBeTruthy();

		const body = await resp.json();
		expect(body.status, 'health.status must be "ok" when the server is wired').toBe('ok');

		// The scrapers array reflects the registry contents bootstrapped by
		// cmd/javinizer-e2e — only "e2emock" is registered (the e2emock
		// binary skips scraper.RegisterAll). A regression that
		// double-registers or fails to register the scraper shows here.
		expect(Array.isArray(body.scrapers), 'health.scrapers must be an array').toBeTruthy();
		expect(body.scrapers, 'health.scrapers must include the e2emock entry').toContain('e2emock');
		expect(
			body.scrapers,
			'health.scrapers must contain ONLY e2emock in the e2e setup',
		).toHaveLength(1);

		// Version metadata is injected by the build (ldflags). In dev it's
		// "v0.0.0-...-dirty" — assert presence + non-empty rather than a
		// specific value so the spec is stable across releases.
		expect(body.version, 'health.version must be non-empty').toBeTruthy();
		expect(body.commit, 'health.commit must be non-empty').toBeTruthy();
		expect(body.build_date, 'health.build_date must be non-empty').toBeTruthy();
	});

	test('GET /health is reachable via the real Vite proxy from the browser context', async ({
		request,
	}) => {
		// Same as above but through the Vite proxy base URL (request fixture's
		// baseURL = Vite dev server, not the backend directly). Proves the
		// e2e Vite config's `/health` proxy entry works — a regression where
		// the proxy misroutes /health (e.g. falls through to SvelteKit's
		// catch-all) would cause this to 404.
		await loginAgainstRealBackend(request); // not strictly required for /health, but exercises the cookie jar

		const resp = await request.get('/health');
		expect(resp.ok(), `proxied /health must return 2xx, got ${resp.status()}`).toBeTruthy();
		const body = await resp.json();
		expect(body.status).toBe('ok');
	});
});
