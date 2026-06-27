import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import path from 'node:path';

export default defineConfig({
	plugins: [svelte({ hot: !process.env.VITEST })],
	resolve: {
		// Resolve `svelte` to its browser (client) build so that
		// @testing-library/svelte can `mount(...)` components under jsdom.
		// Without this, Svelte 5 resolves to the server entry and
		// `mount` is unavailable ("mount(...) is not available on the server").
		conditions: ['browser'],
		alias: {
			$lib: path.resolve(__dirname, 'src/lib'),
		},
	},
	test: {
		include: ['src/**/*.{test,spec}.{js,ts}'],
		environment: 'jsdom',
		globals: true,
	},
});
