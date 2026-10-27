/** Dedicated real dump → private source authority → API → read-only review proof. */
import { defineConfig, devices } from '@playwright/test';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const frontend = dirname(fileURLToPath(import.meta.url));
const repo = resolve(frontend, '../..');
const port = Number(process.env.E2E_EVIDENCE_VITE_PORT ?? 5179);
const storage = resolve(frontend, 'tests/fullstack/.auth/auth-state.json');
// Also inherited by setup workers, not only by the backend child.
process.env.JAVINIZER_E2E_OFFLINE_EVIDENCE = 'true';
process.env.JAVINIZER_E2E_INPUT_DIR ??= '/tmp/javinizer-e2e-offline-evidence-input';
process.env.JAVINIZER_E2E_OUTPUT_DIR ??= '/tmp/javinizer-e2e-offline-evidence-output';
export default defineConfig({
	testDir: './tests/fullstack',
	workers: 1,
	fullyParallel: false,
	retries: 0,
	forbidOnly: true,
	timeout: 90_000,
	globalTimeout: 300_000,
	expect: { timeout: 15_000 },
	outputDir: 'test-results-offline-evidence',
	reporter: [['list']],
	use: {
		baseURL: `http://localhost:${port}`,
		serviceWorkers: 'block',
		trace: 'off',
		video: 'off',
		screenshot: 'only-on-failure',
	},
	webServer: [
		{
			command: process.env.E2E_EVIDENCE_BACKEND_COMMAND ?? resolve(repo, 'bin/javinizer-e2e'),
			cwd: repo,
			url: 'http://127.0.0.1:18080/health',
			timeout: 60_000,
			reuseExistingServer: false,
			env: {
				...process.env,
				JAVINIZER_E2E_OFFLINE_EVIDENCE: 'true',
				JAVINIZER_E2E_PORT: '18080',
				JAVINIZER_E2E_AUTH: 'true',
				JAVINIZER_E2E_USERNAME: 'admin',
				JAVINIZER_E2E_PASSWORD: 'adminpassword123',
			},
		},
		{
			command: `node node_modules/vite/bin/vite.js dev --config vite.fullstack.config.ts --port ${port} --strictPort`,
			cwd: frontend,
			url: `http://localhost:${port}/@vite/client`,
			timeout: 60_000,
			reuseExistingServer: false,
			env: {
				...process.env,
				E2E_BACKEND_TARGET: 'http://127.0.0.1:18080',
				VITE_REVIEW_DETAIL_TIMEOUT_MS: '',
			},
		},
	],
	projects: [
		{
			name: 'setup',
			testMatch: /global-setup\.ts/,
			teardown: 'teardown',
			use: { storageState: undefined },
		},
		{ name: 'teardown', testMatch: /global-teardown\.ts/, use: { storageState: undefined } },
		{
			name: 'chromium',
			testMatch: /regressions\/offline-evidence\.spec\.ts/,
			dependencies: ['setup'],
			use: { ...devices['Desktop Chrome'], storageState: storage },
		},
	],
});
