import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';

// Override the $app/environment stub (which hardcodes browser=false in the
// vitest setup) so isDesktopApp() takes the browser-visible branch. The
// desktop-only tests below additionally stub window.location to the wails:
// scheme via Object.defineProperty (the pattern used by websocket.test.ts).
vi.mock('$app/environment', () => ({ browser: true }));

import { getAPIBaseURL, BaseClient, SystemClient } from './common';

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

describe('SystemClient.withSessionParam', () => {
	const client = new SystemClient('');
	let originalLocation: Location;

	beforeEach(() => {
		originalLocation = window.location;
		BaseClient.setSessionID(null);
	});

	afterEach(() => {
		// Restore the real jsdom location so isDesktopApp() is false again.
		Object.defineProperty(window, 'location', {
			value: originalLocation,
			writable: true,
			configurable: true,
		});
		BaseClient.setSessionID(null);
	});

	function stubDesktopLocation() {
		Object.defineProperty(window, 'location', {
			value: {
				protocol: 'wails:',
				hostname: 'wails.localhost',
				origin: 'wails://wails.localhost',
				href: 'wails://wails.localhost/',
			},
			writable: true,
			configurable: true,
		});
	}

	it('is a no-op in the browser (non-desktop): returns the URL unchanged', () => {
		// jsdom location is not a wails: scheme, so isDesktopApp() is false.
		BaseClient.setSessionID('abc');
		expect(client.withSessionParam('http://localhost:8765/api/v1/temp/image?url=x')).toBe(
			'http://localhost:8765/api/v1/temp/image?url=x',
		);
	});

	it('is a no-op for non-/api/v1/ URLs even when desktop + session present', () => {
		// Force the desktop branch; non-/api/v1/ URLs (e.g. external image URLs)
		// must pass through untouched.
		stubDesktopLocation();
		BaseClient.setSessionID('abc');
		expect(client.withSessionParam('https://example.com/image.jpg')).toBe(
			'https://example.com/image.jpg',
		);
	});

	it('is a no-op when no session is set (desktop branch)', () => {
		stubDesktopLocation();
		BaseClient.setSessionID(null);
		expect(client.withSessionParam('/api/v1/temp/image?url=x')).toBe('/api/v1/temp/image?url=x');
	});

	it('appends ?session= when desktop + session present and the URL has no query', () => {
		stubDesktopLocation();
		BaseClient.setSessionID('abc 123');
		expect(client.withSessionParam('/api/v1/temp/posters/job/ID.jpg')).toBe(
			'/api/v1/temp/posters/job/ID.jpg?session=abc%20123',
		);
	});

	it('appends &session= when desktop + session present and the URL already has a query', () => {
		stubDesktopLocation();
		BaseClient.setSessionID('tok');
		expect(client.withSessionParam('/api/v1/temp/image?url=https://x/y.jpg')).toBe(
			'/api/v1/temp/image?url=https://x/y.jpg&session=tok',
		);
	});
});
