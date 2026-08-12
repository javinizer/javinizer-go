/**
 * Static-flavor Playwright config.
 *
 * The fourth test distinction (see playwright.frontend.config.ts for the
 * map): asserts the SHIPPED deployment flavor — the production adapter-static
 * build embedded into the real Go binary via //go:embed all:dist
 * (scripts/with_embedded_web.sh stages web/frontend/build → web/dist) and
 * served by Gin's NoRoute fallback, exactly as Docker / release binaries
 * serve it. No Vite dev server, no page.route mocks.
 *
 * Webserver: the prebuilt `bin/javinizer-e2e-static` binary (production
 * cmd/javinizer-e2e, e2emock scraper, auto-initialized admin/adminpassword123
 * auth), built by `make test-static-e2e` from staged production assets.
 * reuseExistingServer is intentionally false: this suite exists to pin the
 * CURRENT build output, so it must never silently reuse a stale binary left
 * on the port from a previous run.
 *
 * Run with:
 *   make test-static-e2e
 *   # or, with bin/javinizer-e2e-static already built:
 *   npx playwright test --config=playwright.static.config.ts
 */
import { defineConfig, devices } from '@playwright/test';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const FRONTEND_DIR = process.env.E2E_FRONTEND_DIR ?? resolve(fileURLToPath(import.meta.url), '..');
const REPO_ROOT = process.env.E2E_REPO_ROOT ?? resolve(FRONTEND_DIR, '../..');
const STATIC_PORT = Number(process.env.E2E_STATIC_PORT ?? 18081);
const STATIC_BINARY = process.env.E2E_STATIC_BINARY ?? resolve(REPO_ROOT, 'bin', 'javinizer-e2e-static');

export default defineConfig({
	testDir: './tests/static',
	timeout: 60_000,
	expect: { timeout: 15_000 },
	fullyParallel: false,
	workers: 1,
	forbidOnly: !!process.env.CI,
	retries: 0,
	reporter: process.env.CI ? 'list' : [['list']],
	use: {
		baseURL: `http://localhost:${STATIC_PORT}`,
		trace: 'retain-on-failure',
		video: 'retain-on-failure',
	},
	webServer: {
		name: 'javinizer-static-binary',
		command: `"${STATIC_BINARY}"`,
		cwd: REPO_ROOT,
		url: `http://localhost:${STATIC_PORT}/health`,
		timeout: 60_000,
		reuseExistingServer: false,
		env: {
			...process.env,
			JAVINIZER_E2E_PORT: String(STATIC_PORT),
			JAVINIZER_E2E_INPUT_DIR: process.env.JAVINIZER_E2E_INPUT_DIR ?? '/tmp/javinizer-e2e-static-input',
			JAVINIZER_E2E_OUTPUT_DIR: process.env.JAVINIZER_E2E_OUTPUT_DIR ?? '/tmp/javinizer-e2e-static-output',
		},
	},
	projects: [
		{
			name: 'chromium',
			use: { ...devices['Desktop Chrome'] },
		},
	],
});
