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
			// Proxy WebSocket connections to Go backend during development
			'/ws': {
				target: 'http://localhost:8080',
				ws: true,
				changeOrigin: true,
			},
			// Proxy health check
			'/health': {
				target: 'http://localhost:8080',
				changeOrigin: true,
			},
		},
	},
});
