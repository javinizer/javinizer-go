/**
 * Vite config for full-stack E2E tests.
 *
 * Identical to vite.config.ts (the production dev config) except it points
 * the /api + /ws + /health proxy at port 18080 instead of 8080 — the port
 * cmd/javinizer-e2e binds by default. Keeps the e2e backend isolated from
 * any developer's running `javinizer api` dev instance on 8080.
 */
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const E2E_BACKEND_TARGET = process.env.E2E_BACKEND_TARGET ?? 'http://localhost:18080';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		port: Number(process.env.E2E_VITE_PORT ?? 5175),
		strictPort: true,
		proxy: {
			'/api': {
				target: E2E_BACKEND_TARGET,
				changeOrigin: true,
			},
			'/ws': {
				target: E2E_BACKEND_TARGET,
				ws: true,
				// changeOrigin:false preserves the browser's original Host (the Vite
				// port) so the backend's isSameOrigin check (Origin vs request Host,
				// port-sensitive) passes for a browser-context WS upgrade. With
				// changeOrigin:true the Host is rewritten to :18080 while the browser
				// Origin stays on the Vite port → 403. See vite.config.ts for the
				// same rationale. This lets the fullstack e2e suite exercise the
				// real browser→proxy→backend WS path the app uses.
				changeOrigin: false,
			},
			'/health': {
				target: E2E_BACKEND_TARGET,
				changeOrigin: true,
			},
		},
	},
});
