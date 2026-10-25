import { defineConfig, devices } from '@playwright/test';
import { existsSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const frontend = dirname(fileURLToPath(import.meta.url));
const port = Number(process.env.E2E_SYNC_VITE_PORT ?? 5181);
const backendPort = Number(process.env.E2E_SYNC_BACKEND_PORT ?? 18081);
const executablePath =
	process.env.E2E_CHROMIUM_PATH ??
	(!process.env.CI && existsSync('/usr/bin/chromium') ? '/usr/bin/chromium' : undefined);
export default defineConfig({
	testDir: './tests/sync',
	testMatch: '**/*.spec.ts',
	workers: 1,
	fullyParallel: false,
	retries: 0,
	forbidOnly: !!process.env.CI,
	timeout: 90_000,
	expect: { timeout: 15_000 },
	outputDir: 'test-results-sync',
	reporter: [['list']],
	use: {
		baseURL: `http://localhost:${port}`,
		serviceWorkers: 'block',
		screenshot: 'only-on-failure',
		trace: 'retain-on-failure',
	},
	webServer: {
		command: `npm run dev -- --config vite.fullstack.config.ts --port ${port} --strictPort`,
		cwd: frontend,
		// Root SSR needs a per-test backend that does not exist until the fixture starts.
		url: `http://localhost:${port}/@vite/client`,
		timeout: 120_000,
		reuseExistingServer: false,
		env: {
			...process.env,
			E2E_BACKEND_TARGET: `http://127.0.0.1:${backendPort}`,
			E2E_VITE_PORT: String(port),
			VITE_REVIEW_DETAIL_TIMEOUT_MS: '',
		},
	},
	projects: [
		{
			name: 'sync-chromium',
			use: {
				...devices['Desktop Chrome'],
				launchOptions: executablePath ? { executablePath: resolve(executablePath) } : {},
			},
		},
	],
});
