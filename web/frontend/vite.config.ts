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
				target: 'http://localhost:8765',
				changeOrigin: true,
			},
			// Proxy WebSocket connections to Go backend during development.
			// changeOrigin is FALSE for /ws (unlike /api + /health): the backend's
			// WS upgrader validates Origin via isSameOrigin (scheme+host+port of
			// the Origin header vs the request Host). With changeOrigin:true the
			// Host is rewritten to the backend port (8765) while the browser Origin
			// stays on the Vite port (5174, where make web-dev runs) → port mismatch → 403, so the browser
			// could never open /ws/progress in dev. changeOrigin:false preserves the
			// browser's original Host so Origin and Host match → same-origin allowed.
			// Cookie auth is unaffected (the session cookie is scoped to the Vite
			// origin the browser already holds it for).
			'/ws': {
				target: 'http://localhost:8765',
				ws: true,
				changeOrigin: false,
			},
			// Proxy health check
			'/health': {
				target: 'http://localhost:8765',
				changeOrigin: true,
			},
			// Proxy API docs (Scalar + Swagger UI) so openDocs() in dev reaches the
			// Go backend instead of 404ing on the Vite server.
			'/docs': {
				target: 'http://localhost:8765',
				changeOrigin: true,
			},
			'/swagger': {
				target: 'http://localhost:8765',
				changeOrigin: true,
			},
		},
	},
});
