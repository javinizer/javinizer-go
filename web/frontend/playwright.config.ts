import { defineConfig, devices } from '@playwright/test';

/**
 * Run with dev servers:
 * `go run ./cmd/javinizer api` (port 8080)
 * `make web-dev` (port 5174)
 * `cd web/frontend && npx playwright test`
 */
export default defineConfig({
	testDir: './tests/e2e',
	timeout: 30_000,
	expect: {
		timeout: 10_000
	},
	fullyParallel: true,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 2 : 0,
	workers: process.env.CI ? 1 : undefined,
	reporter: process.env.CI
		? [['list'], ['html', { open: 'never' }]]
		: [['list'], ['html']],
	use: {
		baseURL: process.env.E2E_BASE_URL || 'http://localhost:5174',
		headless: true,
		trace: 'on-first-retry',
		video: 'retain-on-failure',
		locale: 'en-US',
		storageState: process.env.E2E_AUTH_STATE || undefined
	},
	projects: [
		{
			name: 'chromium',
			use: { ...devices['Desktop Chrome'] }
		}
	]
});
