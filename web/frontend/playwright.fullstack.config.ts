/**
 * Full-stack Playwright config.
 *
 * Unlike the existing playwright.config.ts (which mocks all /api routes via
 * page.route), this config spawns REAL backend + frontend processes via
 * Playwright's `webServer` option. Every assertion exercises the real
 * Hauptstrasse: browser → real SvelteKit frontend → real HTTP transport →
 * real Go API server → real worker pipeline → real result tracker → real
 * in-memory SQLite → real mock scraper at the scraper seam.
 *
 * Backend: `go run ./cmd/javinizer-e2e` — boots real api server with only
 * the "e2emock" scraper registered (deterministic per MovieID), :memory:
 * SQLite, auto-init auth admin/adminpassword123, listens on 127.0.0.1:18080
 * (the port vite.fullstack.config.ts's proxy config targets — so the
 * browser hitting Vite on $FRONTEND_PORT transparently proxies API traffic
 * to the real Go binary on 18080).
 *
 * Frontend: `npm run dev -- --config vite.fullstack.config.ts --port $FRONTEND_PORT
 * --strictPort` — Vite dev server with the e2e-specific proxy config that
 * forwards /api + /ws + /health to 18080.
 *
 * Run with:
 *   make test-e2e-fullstack
 *   # or directly:
 *   npx playwright test --config=playwright.fullstack.config.ts
 *
 * Project layout (under tests/fullstack/):
 *   global-setup.ts        — real-backend auth + fixture seeding (setup project)
 *   global-teardown.ts     — cleanup placeholder (teardown project)
 *   helpers/               — shared TypeScript helpers (api, jobs, navigation,
 *                           fixtures, types) — NOT picked up by Playwright's
 *                           default spec-file matcher, so they live
 *                           alongside specs without being run as tests.
 *   regressions/           — spec file groups for the regression class. Add
 *                           new spec groups here when extending the suite
 *                           (e.g. `organize.spec.ts`, `rescrape.spec.ts`,
 *                           `jobs-list.spec.ts`). Keep each spec file
 *                           scoped to one user-visible workflow.
 *
 * To extend with a new spec file:
 *   1. Drop `tests/fullstack/regressions/your-feature.spec.ts`
 *   2. Import helpers from `../helpers` (barrel) or specific submodules.
 *   3. Run: `npx playwright test --config=playwright.fullstack.config.ts
 *      tests/fullstack/regressions/your-feature.spec.ts`.
 */
import { defineConfig, devices } from '@playwright/test';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const FRONTEND_DIR = process.env.E2E_FRONTEND_DIR ?? resolve(fileURLToPath(import.meta.url), '..');
const REPO_ROOT = process.env.E2E_REPO_ROOT ?? resolve(FRONTEND_DIR, '../..'); // web/frontend → javinizer-go root
const FRONTEND_PORT = Number(process.env.E2E_VITE_PORT ?? 5175);
const BACKEND_PORT = 18080; // e2e-only port; isolated from developer's `javinizer api` on 8080

// storageState path — written by global-setup.ts (setup project) once per
// run, reused by the chromium project's `page` + `request` fixtures via
// `use.storageState: STORAGE_PATH`. Lives under tests/fullstack/.auth/ —
// that subdir is gitignored so the real session cookie is never committed.
const CONFIG_DIR = dirname(fileURLToPath(import.meta.url));
const STORAGE_PATH = resolve(CONFIG_DIR, 'tests', 'fullstack', '.auth', 'auth-state.json');

export default defineConfig({
	testDir: './tests/fullstack',
	timeout: 60_000,
	expect: {
		timeout: 15_000,
	},
	fullyParallel: false,
	// Run specs serially (effectively one worker). The fullstack suite drives a
	// SINGLE shared backend (one :memory: SQLite, one global JobStore, one shared
	// job list). Parallel workers mutate that shared state concurrently, which
	// flakes specs that assert on global counts/ordering (e.g. pagination total,
	// delete-then-list, cache-hit per-file status). Serial run is ~40s vs ~11s —
	// an acceptable tradeoff for determinism. If you reintroduce parallelism,
	// specs must assert against their own job IDs (filter, not global state).
	workers: 1,
	forbidOnly: !!process.env.CI,
	retries: 0,
	reporter: process.env.CI ? 'list' : [['list']],
	use: {
		baseURL: `http://localhost:${FRONTEND_PORT}`,
		trace: 'retain-on-failure',
		video: 'retain-on-failure',
	},
	// Two real processes are spawned here.
	// - Backend first (Go binary listening on $BACKEND_PORT).
	// - Frontend second (Vite dev server on $FRONTEND_PORT).
	// Both are killed automatically when Playwright tears down the run.
	webServer: [
		{
			name: 'javinizer-e2e-backend',
			command: `go run ${REPO_ROOT}/cmd/javinizer-e2e`,
			cwd: REPO_ROOT,
			port: BACKEND_PORT,
			// /health returns 200 once the Gin server is fully wired
			// (routes registered, DB migrated, scraper registry populated).
			// The Go binary prints "listening on http://..." to stdout but
			// curl-polling /health is the canonical readiness signal.
			curlOnStart: true,
			timeout: 120_000, // first `go run` compile can take ~30s
			reuseExistingServer: !process.env.CI,
			env: {
				...process.env,
				JAVINIZER_E2E_PORT: String(BACKEND_PORT),
				JAVINIZER_E2E_INPUT_DIR: process.env.JAVINIZER_E2E_INPUT_DIR ?? '/tmp/javinizer-e2e-input',
				JAVINIZER_E2E_OUTPUT_DIR:
					process.env.JAVINIZER_E2E_OUTPUT_DIR ?? '/tmp/javinizer-e2e-output',
				// Real e2emock scraper auto-initializes the auth user.
				JAVINIZER_E2E_AUTH: 'true',
				JAVINIZER_E2E_USERNAME: 'admin',
				JAVINIZER_E2E_PASSWORD: 'adminpassword123',
			},
		},
		{
			name: 'javinizer-e2e-frontend',
			command: `npm run dev -- --config vite.fullstack.config.ts --port ${FRONTEND_PORT} --strictPort`,
			cwd: FRONTEND_DIR,
			port: FRONTEND_PORT,
			timeout: 60_000,
			reuseExistingServer: !process.env.CI,
		},
	],
	projects: [
		{
			name: 'setup',
			testMatch: /global-setup\.ts/,
			teardown: 'teardown',
			// setup project does NOT inherit storageState — it CREATES the file.
			use: { storageState: undefined },
		},
		{
			name: 'teardown',
			testMatch: /global-teardown\.ts/,
			use: { storageState: undefined },
		},
		{
			name: 'chromium',
			use: {
				...devices['Desktop Chrome'],
				// Populated by the setup project's global-setup.ts — both the
				// browser context AND the `request` fixture inherit these
				// cookies, so all subsequent specs are authenticated against
				// the real Go backend WITHOUT per-spec re-auth.
				storageState: STORAGE_PATH,
			},
			dependencies: ['setup'],
		},
	],
});
