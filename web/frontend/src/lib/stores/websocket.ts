import { writable } from 'svelte/store';
import { browser } from '$app/environment';
import type { ProgressMessage } from '$lib/api/types';
import { toastStore } from '$lib/stores/toast';

// Build WebSocket URL dynamically from browser location
// This works for both local dev (localhost:8080) and Docker (any host)
// Converts http -> ws and https -> wss automatically
function getWebSocketURL(): string {
	if (!browser) {
		// During SSR, return a placeholder (won't be used)
		return 'ws://localhost:8080/ws/progress';
	}
	// Replace http/https with ws/wss and append the WebSocket path
	return location.origin.replace(/^http/, 'ws') + '/ws/progress';
}

interface WebSocketState {
	connected: boolean;
	messages: ProgressMessage[];
	messagesByFile: Record<string, Record<string, ProgressMessage>>; // Latest message per file per job (job_id -> file_path -> message)
	error?: string;
}

function createWebSocketStore() {
	const { subscribe, set, update } = writable<WebSocketState>({
		connected: false,
		messages: [],
		messagesByFile: {}
	});

	let ws: WebSocket | null = null;
	let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
	let shouldReconnect = false;
	let lastErrorToastTime = 0;

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

		const wsUrl = getWebSocketURL();

		try {
			ws = new WebSocket(wsUrl);

			ws.onopen = () => {
				update((state) => ({ ...state, connected: true, error: undefined }));
			};

			ws.onclose = () => {
				update((state) => ({ ...state, connected: false }));
				ws = null;

				if (!shouldReconnect) {
					return;
				}

				// Attempt to reconnect after 3 seconds
				reconnectTimeout = setTimeout(() => {
					connect();
				}, 3000);
			};

			ws.onerror = (error) => {
				console.error('WebSocket error:', error);
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
							messages: [...state.messages, message],
							messagesByFile: newMessagesByFile
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
		}
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

		set({ connected: false, messages: [], messagesByFile: {} });
	}

	function clearMessages() {
		update((state) => ({ ...state, messages: [], messagesByFile: {} }));
	}

	return {
		subscribe,
		connect,
		disconnect,
		clearMessages
	};
}

export const websocketStore = createWebSocketStore();
