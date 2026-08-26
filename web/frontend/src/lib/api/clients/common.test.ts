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

describe('BaseClient.request lifecycle', () => {
	const client = new BaseClient('http://api.test');
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		fetchMock = vi.fn();
		vi.stubGlobal('fetch', fetchMock);
		BaseClient.setSessionID(null);
	});

	afterEach(() => {
		vi.useRealTimers();
		vi.unstubAllGlobals();
		BaseClient.setSessionID(null);
	});

	function okResponse(body = '{"ok":true}') {
		return { ok: true, text: vi.fn().mockResolvedValue(body) } as unknown as Response;
	}

	function abortError() {
		return new DOMException('The operation was aborted.', 'AbortError');
	}

	it('forwards the caller signal and preserves desktop session request fields', async () => {
		fetchMock.mockResolvedValue(okResponse());
		BaseClient.setSessionID('session-123');
		const controller = new AbortController();

		await expect(client.request('/batch', { signal: controller.signal })).resolves.toEqual({ ok: true });

		const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
		expect(init.credentials).toBe('same-origin');
		expect(init.signal).toBe(controller.signal);
		expect(init.headers).toEqual({
			'Content-Type': 'application/json',
			'X-Session-ID': 'session-123',
		});
	});

	it('times out before response headers and classifies the error', async () => {
		vi.useFakeTimers();
		fetchMock.mockImplementation((_url: string, init: RequestInit) =>
			new Promise((_resolve, reject) => {
				init.signal?.addEventListener('abort', () => reject(abortError()), { once: true });
			}),
		);

		const request = client.request('/slow', { timeoutMs: 10 });
		const assertion = expect(request).rejects.toMatchObject({ name: 'ApiError', code: 'REQUEST_TIMEOUT' });
		await vi.advanceTimersByTimeAsync(10);

		await assertion;
		expect(vi.getTimerCount()).toBe(0);
	});

	it('times out while the response body is stalled', async () => {
		vi.useFakeTimers();
		let rejectBody: ((reason: unknown) => void) | undefined;
		fetchMock.mockImplementation((_url: string, init: RequestInit) => {
			const body = new Promise<string>((_resolve, reject) => {
				rejectBody = reject;
				init.signal?.addEventListener('abort', () => reject(abortError()), { once: true });
			});
			return Promise.resolve({ ok: true, text: () => body } as unknown as Response);
		});

		const request = client.request('/body-stall', { timeoutMs: 10 });
		const assertion = expect(request).rejects.toMatchObject({ code: 'REQUEST_TIMEOUT' });
		await vi.advanceTimersByTimeAsync(10);
		await assertion;
		expect(rejectBody).toBeDefined();
		expect(vi.getTimerCount()).toBe(0);
	});

	it('preserves caller cancellation and does not relabel it as a timeout', async () => {
		vi.useFakeTimers();
		fetchMock.mockImplementation((_url: string, init: RequestInit) =>
			new Promise((_resolve, reject) => {
				init.signal?.addEventListener('abort', () => reject(abortError()), { once: true });
			}),
		);
		const controller = new AbortController();
		const request = client.request('/cancel', { signal: controller.signal, timeoutMs: 50 });

		controller.abort();
		await expect(request).rejects.toMatchObject({ name: 'AbortError' });
		await vi.advanceTimersByTimeAsync(50);
		expect(vi.getTimerCount()).toBe(0);
	});

	it('preserves caller cancellation while reading a non-success response body', async () => {
		vi.useFakeTimers();
		const controller = new AbortController();
		fetchMock.mockResolvedValue({
			ok: false,
			status: 500,
			statusText: 'Server Error',
			json: () => {
				if (controller.signal.aborted) return Promise.reject(abortError());
				return new Promise((_resolve, reject) => {
					controller.signal.addEventListener('abort', () => reject(abortError()), { once: true });
				});
			},
		} as unknown as Response);

		const request = client.request('/error-body', { signal: controller.signal, timeoutMs: 50 });
		controller.abort();
		await expect(request).rejects.toMatchObject({ name: 'AbortError' });
		await vi.advanceTimersByTimeAsync(50);
		expect(vi.getTimerCount()).toBe(0);
	});

	it('classifies timeout before a later caller cancellation', async () => {
		vi.useFakeTimers();
		fetchMock.mockImplementation((_url: string, init: RequestInit) =>
			new Promise((_resolve, reject) => {
				init.signal?.addEventListener('abort', () => reject(abortError()), { once: true });
			}),
		);
		const controller = new AbortController();
		const request = client.request('/timeout-first', { signal: controller.signal, timeoutMs: 10 });
		const assertion = expect(request).rejects.toMatchObject({ code: 'REQUEST_TIMEOUT' });

		await vi.advanceTimersByTimeAsync(10);
		controller.abort();
		await assertion;
		expect(vi.getTimerCount()).toBe(0);
	});

	it('clears the timeout after a successful response', async () => {
		vi.useFakeTimers();
		fetchMock.mockResolvedValue(okResponse());

		await expect(client.request('/fast', { timeoutMs: 50 })).resolves.toEqual({ ok: true });
		expect(vi.getTimerCount()).toBe(0);
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
