import { defineConfig } from 'vite';

// Serve the pinned subject's adapter-static output. Deliberately do not load
// SvelteKit/Paraglide plugins here: the production build has already generated
// messages and compiled the UI, so startup needs no dev SSR module transport.
export default defineConfig({
	appType: 'spa', // index.html fallback also serves /browse and /review/:jobId.
	build: { outDir: 'build' },
	preview: {
		host: 'localhost',
		port: 5175,
		strictPort: true,
		proxy: {
			'/api': { target: 'http://127.0.0.1:18080', changeOrigin: true },
			'/ws': {
				target: 'http://127.0.0.1:18080',
				ws: true,
				// Preserve Host so the backend's same-origin WebSocket check passes.
				changeOrigin: false,
			},
			'/health': { target: 'http://127.0.0.1:18080', changeOrigin: true },
		},
	},
});
