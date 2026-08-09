/**
 * Frontend-only Playwright config.
 *
 * One of four test distinctions:
 *   1. Frontend-only  — this config. Mocked API (page.route), no backend.
 *      Fast, deterministic, CI-runnable. Spawns only the Vite dev server.
 *   2. Full e2e       — playwright.fullstack.config.ts. Real backend + real
 *      transport + real SQLite. CI-runnable (`make test-e2e-fullstack`).
 *   3. Static-flavor  — playwright.static.config.ts. Production adapter-static
 *      build embedded in the real Go binary (`make test-static-e2e`).
 *   4. Local-only     — test/e2e/live/. Real JAV sites over the public
 *      internet. Never in CI (`make test-e2e-live`, local dev only).
 *
 * What this config pins: pure frontend rendering + interaction logic with
 * the API fully mocked via page.route — component states, env-gated UI (e.g.
 * install_environment=desktop), error/empty/loading states, keyboard nav —
 * without the latency or non-determinism of a real backend. Specs MUST mock
 * every /api endpoint they touch (including /api/v1/auth/status, which both
 * the universal +layout load's browser branch and the shared +layout onMount
 * call from the browser) so no real backend is required.
 *
 * webServer spawns Vite (no backend). The regular vite.config.ts is fine:
 * page.route intercepts browser-side /api requests before Vite's proxy sees
 * them. The one path page.route CANNOT intercept is the dev-SSR branch of the
 * universal +layout load (server-side event.fetch on initial document loads,
 * falling through to the :8765 proxy): with no backend it fails fast to a
 * null authStatus and the page.route-mocked onMount retry governs assertions;
 * if you keep a real backend on :8765 while running this suite, SSR HTML can
 * carry genuine auth state — keep assertions post-hydration/mocked.
 *
 * Run with:
 *   make test-e2e-frontend
 *   # or directly:
 *   npx playwright test --config=playwright.frontend.config.ts
 */
import { defineConfig, devices } from '@playwright/test';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = fileURLToPath(new URL('.', import.meta.url));
const FRONTEND_DIR = process.env.E2E_FRONTEND_DIR ?? resolve(__dirname);
const FRONTEND_PORT = Number(process.env.E2E_VITE_PORT ?? 5176);

export default defineConfig({
	testDir: './tests/frontend',
	timeout: 30_000,
	expect: { timeout: 10_000 },
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	reporter: process.env.CI ? 'list' : [['list']],
	use: {
		baseURL: `http://localhost:${FRONTEND_PORT}`,
		trace: 'on-first-retry',
		video: 'retain-on-failure',
	},
	projects: [
		{
			name: 'chromium',
			use: { ...devices['Desktop Chrome'] },
		},
	],
	webServer: {
		command: `npm run dev -- --port ${FRONTEND_PORT} --strictPort`,
		url: `http://localhost:${FRONTEND_PORT}`,
		cwd: FRONTEND_DIR,
		reuseExistingServer: !process.env.CI,
		timeout: 60_000,
	},
});
