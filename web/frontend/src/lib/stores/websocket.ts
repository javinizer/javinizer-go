import { writable } from 'svelte/store';
import { browser } from '$app/environment';
import type { ProgressMessage } from '$lib/api/types';
import { toastStore } from '$lib/stores/toast';
import { BaseClient } from '$lib/api/clients/common';

// Build WebSocket URL dynamically from browser location.
// In the browser (dev server / Docker), the SPA and API are same-origin, so
// the WS URL is derived from location.origin. In the desktop app the Wails
// AssetServer returns 501 for WS upgrades, so the frontend cannot use a
// same-origin WS URL through the reverse proxy; instead it fetches the direct
// WS URL (ws://localhost:PORT/ws/progress) from GET /desktop/runtime, which
// the desktop reverse proxy serves without forwarding.

function isDesktopApp(): boolean {
	if (!browser) return false;
	if (location.protocol === 'wails:') return true;
	return location.hostname === 'wails.localhost';
}

function sameOriginWebSocketURL(): string {
	// Replace http/https with ws/wss and append the WebSocket path.
	return location.origin.replace(/^http/, 'ws') + '/ws/progress';
}

// Append the session ID as a query parameter. The browser cannot set custom
// headers (e.g. X-Session-ID) on a WebSocket, and in the desktop app the
// session cookie is stored against the webview origin — not 127.0.0.1:PORT —
// so it is not sent on a direct WS connection. The auth middleware falls back
// to the ?session= query parameter, which is how the desktop app authenticates
// the WS upgrade.
function withSessionParam(base: string): string {
	const sid = BaseClient.getSessionID();
	if (!sid) return base;
	const sep = base.includes('?') ? '&' : '?';
	return `${base}${sep}session=${encodeURIComponent(sid)}`;
}

interface WebSocketState {
	connected: boolean;
	skipped: boolean;
	messages: ProgressMessage[];
	messagesByFile: Record<string, Record<string, ProgressMessage>>; // Latest message per file per job (job_id -> file_path -> message)
	error?: string;
}

function createWebSocketStore() {
	const { subscribe, set, update } = writable<WebSocketState>({
		connected: false,
		skipped: false,
		messages: [],
		messagesByFile: {},
	});

	let ws: WebSocket | null = null;
	let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
	let shouldReconnect = false;
	let lastErrorToastTime = 0;
	// Cached direct WS URL for the desktop app. The port is fixed for the
	// app's lifetime, so fetch once and reuse across reconnects.
	let desktopWSUrl: string | null = null;

	async function resolveDesktopWSUrl(): Promise<string | null> {
		if (desktopWSUrl) return desktopWSUrl;
		try {
			const resp = await fetch('/desktop/runtime');
			if (!resp.ok) return null;
			const data = (await resp.json()) as { ws_url?: string };
			if (typeof data.ws_url === 'string' && data.ws_url.length > 0) {
				desktopWSUrl = data.ws_url;
				return desktopWSUrl;
			}
		} catch {
			// fall through to caller's error handling
		}
		return null;
	}

	function scheduleReconnect() {
		if (!shouldReconnect || reconnectTimeout) return;
		reconnectTimeout = setTimeout(() => {
			reconnectTimeout = null;
			connect();
		}, 3000);
	}

	function openSocket(wsUrl: string) {
		try {
			ws = new WebSocket(wsUrl);

			ws.onopen = () => {
				update((state) => ({ ...state, connected: true, error: undefined }));
			};

			ws.onclose = () => {
				update((state) => ({ ...state, connected: false }));
				ws = null;
				scheduleReconnect();
			};

			ws.onerror = () => {
				const now = Date.now();
				if (now - lastErrorToastTime > 10000) {
					toastStore.error('WebSocket connection error');
					lastErrorToastTime = now;
				}
				update((state) => ({ ...state, error: 'WebSocket connection error' }));
			};

			ws.onmessage = (event) => {
				try {
					const message: ProgressMessage = JSON.parse(event.data);
					update((state) => {
						const newMessagesByFile = { ...state.messagesByFile };
						if (message.file_path && message.job_id) {
							// Deduplicate by keeping only the latest message per file per job
							if (!newMessagesByFile[message.job_id]) {
								newMessagesByFile[message.job_id] = {};
							}
							newMessagesByFile[message.job_id][message.file_path] = message;
						}
						return {
							...state,
							messages: [...state.messages, message].slice(-999),
							messagesByFile: newMessagesByFile,
						};
					});
				} catch (error) {
					console.error('Failed to parse WebSocket message:', error);
					toastStore.error('Failed to process server message');
				}
			};
		} catch (error) {
			console.error('Failed to create WebSocket:', error);
			toastStore.error('Failed to connect to server');
			update((state) => ({ ...state, error: 'Failed to create WebSocket connection' }));
			scheduleReconnect();
		}
	}

	function connect() {
		if (!browser) {
			console.warn('WebSocket connection attempted during SSR, skipping');
			return;
		}

		shouldReconnect = true;

		if (reconnectTimeout) {
			clearTimeout(reconnectTimeout);
			reconnectTimeout = null;
		}

		if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
			return;
		}

		if (isDesktopApp()) {
			// The Wails AssetServer returns 501 for WS upgrades, so connect
			// directly to the embedded API server's WS endpoint. The URL (with
			// the random localhost port) is served by the desktop reverse proxy
			// at /desktop/runtime.
			resolveDesktopWSUrl()
				.then((base) => {
					// Re-check after the async gap: disconnect() may have run while
					// the fetch was in flight (e.g. component unmount), in which
					// case opening a socket now would leak a connection nothing
					// can close. Also guards against overlapping connect() calls.
					if (!shouldReconnect) return;
					if (!base) {
						update((state) => ({ ...state, error: 'WebSocket URL unavailable' }));
						scheduleReconnect();
						return;
					}
					openSocket(withSessionParam(base));
				})
				.catch(() => {
					if (shouldReconnect) scheduleReconnect();
				});
			return;
		}

		openSocket(withSessionParam(sameOriginWebSocketURL()));
	}

	function disconnect() {
		shouldReconnect = false;

		if (reconnectTimeout) {
			clearTimeout(reconnectTimeout);
			reconnectTimeout = null;
		}

		if (ws) {
			ws.onclose = null;
			ws.onerror = null;
			ws.onopen = null;
			ws.onmessage = null;
			ws.close();
			ws = null;
		}

		set({ connected: false, skipped: false, messages: [], messagesByFile: {} });
	}

	function clearMessages() {
		update((state) => ({ ...state, messages: [], messagesByFile: {} }));
	}

	return {
		subscribe,
		connect,
		disconnect,
		clearMessages,
	};
}

export const websocketStore = createWebSocketStore();
