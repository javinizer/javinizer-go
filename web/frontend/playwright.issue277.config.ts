import { defineConfig, devices } from '@playwright/test';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// Explicit fixture roots are required, particularly on native Windows. Never
// run this diagnostic against user files. Backend and production UI must be prebuilt.
for (const key of [
	'JAVINIZER_E2E_INPUT_DIR',
	'JAVINIZER_E2E_OUTPUT_DIR',
	'ISSUE277_BACKEND_BINARY',
]) {
	if (!process.env[key]) throw new Error(`${key} is required for this isolated diagnostic`);
}
const frontend = dirname(fileURLToPath(import.meta.url));
const state = resolve(frontend, 'test-results-issue277/auth-state.json');
export default defineConfig({
	testDir: './tests/fullstack',
	timeout: 90_000,
	expect: { timeout: 15_000 },
	fullyParallel: false,
	workers: 1,
	retries: 0,
	forbidOnly: true,
	outputDir: 'test-results-issue277/runs',
	reporter: [['list'], ['json', { outputFile: 'test-results-issue277/results.json' }]],
	use: {
		baseURL: 'http://localhost:5175',
		serviceWorkers: 'block',
		screenshot: 'only-on-failure',
		trace: 'off',
		video: 'off',
	},
	webServer: [
		{
			name: 'issue277-real-backend',
			command: `"${process.env.ISSUE277_BACKEND_BINARY}"`,
			cwd: resolve(frontend, '../..'),
			url: 'http://127.0.0.1:18080/health',
			timeout: 120_000,
			reuseExistingServer: false,
			env: {
				...process.env,
				JAVINIZER_E2E_PORT: '18080',
				JAVINIZER_E2E_AUTH: 'true',
				JAVINIZER_E2E_USERNAME: 'admin',
				JAVINIZER_E2E_PASSWORD: 'adminpassword123',
			},
		},
		{
			name: 'issue277-real-frontend',
			command: 'npm run preview -- --config vite.issue277-preview.config.ts',
			cwd: frontend,
			url: 'http://localhost:5175',
			timeout: 120_000,
			reuseExistingServer: false,
		},
	],
	projects: [
		{ name: 'issue277-setup', testMatch: /issue277-setup\.ts/, use: { storageState: undefined } },
		{
			name: 'issue277-chromium',
			testMatch: /issue277-occupied-video\.diagnostic\.ts/,
			use: { ...devices['Desktop Chrome'], storageState: state },
			dependencies: ['issue277-setup'],
		},
	],
});
