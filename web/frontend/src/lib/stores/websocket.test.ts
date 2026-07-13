import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get } from 'svelte/store';
import type { ProgressMessage } from '$lib/api/types';

// Override the $app/environment stub (which hardcodes browser=false) so the
// store takes the desktop branch. The wails: protocol makes isDesktopApp()
// return true.
vi.mock('$app/environment', () => ({ browser: true }));

// Avoid toast side effects and provide a stable session id for the WS URL.
vi.mock('$lib/stores/toast', () => ({
	toastStore: { error: vi.fn(), success: vi.fn(), info: vi.fn(), warning: vi.fn() },
}));
vi.mock('$lib/api/clients/common', () => ({
	BaseClient: { getSessionID: () => 'test-session' },
}));

// jsdom doesn't implement WebSocket; provide a minimal mock that records
// constructions and lets a test drive readyState transitions.
let constructedURLs: string[] = [];

class MockWebSocket {
	static OPEN = 1;
	static CONNECTING = 0;
	static CLOSING = 2;
	static CLOSED = 3;
	static instances: MockWebSocket[] = [];
	readyState = 0;
	onopen: (() => void) | null = null;
	onclose: (() => void) | null = null;
	onerror: (() => void) | null = null;
	onmessage: ((ev: { data: string }) => void) | null = null;
	constructor(public url: string) {
		constructedURLs.push(url);
		MockWebSocket.instances.push(this);
	}
	close() {
		this.readyState = 3;
		this.onclose?.();
	}
}

function makeProgressMessage(jobID: string, filePath: string): ProgressMessage {
	return {
		job_id: jobID,
		file_index: 0,
		file_path: filePath,
		status: 'success',
		progress: 100,
		message: 'done',
	};
}

describe('websocket store — desktop async-gap guard', () => {
	let originalFetch: typeof globalThis.fetch;
	let originalWebSocket: typeof globalThis.WebSocket;

	beforeEach(() => {
		// Fresh singleton per test: clears the cached desktopWSUrl + shouldReconnect
		// so tests are independent.
		vi.resetModules();
		constructedURLs = [];
		MockWebSocket.instances = [];
		originalFetch = globalThis.fetch;
		originalWebSocket = globalThis.WebSocket;
		// @ts-expect-error installing a minimal mock
		globalThis.WebSocket = MockWebSocket;
		// Pretend we're in the Wails webview so isDesktopApp() is true.
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
	});

	afterEach(() => {
		globalThis.fetch = originalFetch;
		globalThis.WebSocket = originalWebSocket;
		vi.restoreAllMocks();
	});

	it('opens a socket when /desktop/runtime resolves with a ws_url', async () => {
		let resolveFetch!: (v: { ok: boolean; json: () => Promise<unknown> }) => void;
		globalThis.fetch = vi.fn(
			() =>
				new Promise((r) => {
					resolveFetch = r;
				}),
		) as unknown as typeof globalThis.fetch;

		const { websocketStore } = await import('$lib/stores/websocket');
		websocketStore.connect();

		// While the fetch is in flight, no socket yet.
		expect(constructedURLs).toHaveLength(0);

		resolveFetch({
			ok: true,
			json: () => Promise.resolve({ ws_url: 'ws://localhost:9999/ws/progress' }),
		});
		await new Promise((r) => setTimeout(r, 0));

		expect(constructedURLs).toHaveLength(1);
		expect(constructedURLs[0]).toContain('ws://localhost:9999/ws/progress');
		expect(constructedURLs[0]).toContain('session=test-session');
		websocketStore.disconnect();
	});

	it('does NOT open a socket if disconnect() runs during the /desktop/runtime fetch', async () => {
		let resolveFetch!: (v: { ok: boolean; json: () => Promise<unknown> }) => void;
		globalThis.fetch = vi.fn(
			() =>
				new Promise((r) => {
					resolveFetch = r;
				}),
		) as unknown as typeof globalThis.fetch;

		const { websocketStore } = await import('$lib/stores/websocket');
		websocketStore.connect();

		// Component unmount / explicit disconnect while the fetch is still
		// pending — the guard must abort openSocket so no socket leaks.
		websocketStore.disconnect();

		resolveFetch({
			ok: true,
			json: () => Promise.resolve({ ws_url: 'ws://localhost:9999/ws/progress' }),
		});
		await new Promise((r) => setTimeout(r, 0));

		expect(constructedURLs).toHaveLength(0);
	});

	it('schedules a reconnect when /desktop/runtime resolves with no ws_url', async () => {
		globalThis.fetch = vi.fn(() =>
			Promise.resolve({
				ok: true,
				json: () => Promise.resolve({ ws_url: '' }),
			}),
		) as unknown as typeof globalThis.fetch;

		const { websocketStore } = await import('$lib/stores/websocket');
		websocketStore.connect();
		await new Promise((r) => setTimeout(r, 0));

		// No socket opened (no URL), and no leak.
		expect(constructedURLs).toHaveLength(0);
		// A reconnect is scheduled (3s); disconnect to cancel it.
		websocketStore.disconnect();
	});

	it('schedules a reconnect when /desktop/runtime fetch rejects', async () => {
		globalThis.fetch = vi.fn(() =>
			Promise.reject(new Error('network')),
		) as unknown as typeof globalThis.fetch;

		const { websocketStore } = await import('$lib/stores/websocket');
		websocketStore.connect();
		await new Promise((r) => setTimeout(r, 0));

		// Fetch failed: no socket, no leak.
		expect(constructedURLs).toHaveLength(0);
		websocketStore.disconnect();
	});

	it('clears only messages for the requested job', async () => {
		globalThis.fetch = vi.fn(() =>
			Promise.resolve({
				ok: true,
				json: () => Promise.resolve({ ws_url: 'ws://localhost:9999/ws/progress' }),
			}),
		) as unknown as typeof globalThis.fetch;

		const { websocketStore } = await import('$lib/stores/websocket');
		websocketStore.connect();
		await new Promise((r) => setTimeout(r, 0));

		const socket = MockWebSocket.instances[0];
		const dumpMessage = makeProgressMessage('r18dev-dump-download', '/dump.db');
		const batchMessage = makeProgressMessage('batch-job', '/movie.mp4');
		socket.onmessage?.({ data: JSON.stringify(dumpMessage) });
		socket.onmessage?.({ data: JSON.stringify(batchMessage) });

		websocketStore.clearMessages('r18dev-dump-download');

		const state = get({ subscribe: websocketStore.subscribe });
		expect(state.messages).toEqual([batchMessage]);
		expect(state.messagesByFile).toEqual({
			'batch-job': { '/movie.mp4': batchMessage },
		});
		websocketStore.disconnect();
	});

	it('clears all messages when no job is specified', async () => {
		globalThis.fetch = vi.fn(() =>
			Promise.resolve({
				ok: true,
				json: () => Promise.resolve({ ws_url: 'ws://localhost:9999/ws/progress' }),
			}),
		) as unknown as typeof globalThis.fetch;

		const { websocketStore } = await import('$lib/stores/websocket');
		websocketStore.connect();
		await new Promise((r) => setTimeout(r, 0));

		const socket = MockWebSocket.instances[0];
		socket.onmessage?.({ data: JSON.stringify(makeProgressMessage('batch-job', '/movie.mp4')) });
		websocketStore.clearMessages();

		const state = get({ subscribe: websocketStore.subscribe });
		expect(state.messages).toEqual([]);
		expect(state.messagesByFile).toEqual({});
		websocketStore.disconnect();
	});
});
