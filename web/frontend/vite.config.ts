import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	build: {
		rollupOptions: {
			maxParallelFileOps: 1000,
		},
	},
	server: {
		proxy: {
			// Proxy API requests to Go backend during development
			'/api': {
				target: 'http://localhost:8080',
				changeOrigin: true,
			},
			// Proxy WebSocket connections to Go backend during development.
			// changeOrigin is FALSE for /ws (unlike /api + /health): the backend's
			// WS upgrader validates Origin via isSameOrigin (scheme+host+port of
			// the Origin header vs the request Host). With changeOrigin:true the
			// Host is rewritten to the backend port (8080) while the browser Origin
			// stays on the Vite port (5173) → port mismatch → 403, so the browser
			// could never open /ws/progress in dev. changeOrigin:false preserves the
			// browser's original Host so Origin and Host match → same-origin allowed.
			// Cookie auth is unaffected (the session cookie is scoped to the Vite
			// origin the browser already holds it for).
			'/ws': {
				target: 'http://localhost:8080',
				ws: true,
				changeOrigin: false,
			},
			// Proxy health check
			'/health': {
				target: 'http://localhost:8080',
				changeOrigin: true,
			},
		},
	},
});
