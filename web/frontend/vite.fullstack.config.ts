/**
 * Vite config for full-stack E2E tests.
 *
 * Identical to vite.config.ts (the production dev config) except it points
 * the /api + /ws + /health proxy at port 18080 instead of 8080 — the port
 * cmd/javinizer-e2e binds by default. Keeps the e2e backend isolated from
 * any developer's running `javinizer api` dev instance on 8080.
 *
 * It also overrides VITE_API_URL / VITE_WS_URL to empty so the frontend
 * issues relative requests (e2emock flow) instead of absolute ones. The
 * repo's .env hardcodes VITE_API_URL=http://localhost:8080 for normal dev,
 * which would bypass this config's proxy and point the browser straight at
 * :8080 (where no e2e backend listens). Empty values make getAPIBaseURL()
 * return '' so every /api + /ws request is relative → forwarded by the
 * proxy below to the e2e backend on 18080.
 */
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const E2E_BACKEND_TARGET = process.env.E2E_BACKEND_TARGET ?? 'http://localhost:18080';

export default defineConfig({
	plugins: [sveltekit()],
	// Override the .env-provided absolute API/WS URLs so the frontend uses
	// relative paths and the proxy below routes them to the e2e backend.
	// `import.meta.env.VITE_*` is statically replaced at build/dev time, so
	// an empty string here wins over .env's http://localhost:8080.
	// JSON.stringify emits a valid JS string literal (bare '' is rejected by
	// esbuild as an invalid define value).
	define: {
		'import.meta.env.VITE_API_URL': JSON.stringify(''),
		'import.meta.env.VITE_WS_URL': JSON.stringify(''),
	},
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
