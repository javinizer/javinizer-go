import { defineConfig } from '@playwright/test';
import baseConfig from './playwright.fullstack.config';

const REVIEW_FRONTEND_PORT = Number(process.env.E2E_REVIEW_VITE_PORT ?? 5176);
const baseWebServers = Array.isArray(baseConfig.webServer) ? baseConfig.webServer : [];

const webServer = baseWebServers.map((server) => {
	if (server.name === 'javinizer-e2e-frontend') {
		return {
			...server,
			command: `npm run dev -- --config vite.fullstack.config.ts --port ${REVIEW_FRONTEND_PORT} --strictPort`,
			port: REVIEW_FRONTEND_PORT,
			reuseExistingServer: false,
			env: {
				...(server.env ?? {}),
				VITE_REVIEW_DETAIL_TIMEOUT_MS: '50',
			},
		};
	}
	return { ...server, reuseExistingServer: false };
});

export default defineConfig({
	...baseConfig,
	testIgnore: [],
	testMatch: /review-load-timeout\.spec\.ts/,
	use: {
		...baseConfig.use,
		baseURL: `http://localhost:${REVIEW_FRONTEND_PORT}`,
	},
	webServer,
});
