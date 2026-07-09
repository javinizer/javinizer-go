import { describe, it, expect, vi, afterEach } from 'vitest';
import { getAPIBaseURL } from './common';

describe('getAPIBaseURL', () => {
	afterEach(() => {
		vi.unstubAllEnvs();
	});

	it('returns VITE_API_URL in dev mode when the env is set', () => {
		vi.stubEnv('DEV', true);
		vi.stubEnv('VITE_API_URL', 'http://localhost:8765');
		expect(getAPIBaseURL()).toBe('http://localhost:8765');
	});

	it('returns empty string (same-origin) in production even when VITE_API_URL is baked into the bundle', () => {
		// Regression: a production build with VITE_API_URL baked in would pin
		// the API client to a fixed host:port that won't match the server's
		// actual bind address. The desktop binary's `web` subcommand reads the
		// portable config's random port (e.g. 58915), but the baked-in env
		// says :8765 → "Failed to fetch". Gating on import.meta.env.DEV
		// ensures production builds use same-origin relative URLs.
		vi.stubEnv('DEV', false);
		vi.stubEnv('VITE_API_URL', 'http://localhost:8765');
		expect(getAPIBaseURL()).toBe('');
	});

	it('returns empty string when VITE_API_URL is not set (same-origin default)', () => {
		vi.stubEnv('DEV', true);
		vi.stubEnv('VITE_API_URL', '');
		expect(getAPIBaseURL()).toBe('');
	});
});
