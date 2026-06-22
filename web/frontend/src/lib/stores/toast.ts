import { writable } from 'svelte/store';

export interface Toast {
	id: string;
	type: 'success' | 'error' | 'info' | 'warning';
	message: string;
	duration?: number;
}

function createToastStore() {
	const { subscribe, update } = writable<Toast[]>([]);

	let idCounter = 0;

	return {
		subscribe,
		success: (message: string, duration = 5000) => {
			const id = `toast-${++idCounter}`;
			update((toasts) => [...toasts, { id, type: 'success', message, duration }]);
			return id;
		},
		error: (message: string, duration = 7000) => {
			const id = `toast-${++idCounter}`;
			update((toasts) => [...toasts, { id, type: 'error', message, duration }]);
			return id;
		},
		info: (message: string, duration = 5000) => {
			const id = `toast-${++idCounter}`;
			update((toasts) => [...toasts, { id, type: 'info', message, duration }]);
			return id;
		},
		warning: (message: string, duration = 6000) => {
			const id = `toast-${++idCounter}`;
			update((toasts) => [...toasts, { id, type: 'warning', message, duration }]);
			return id;
		},
		dismiss: (id: string) => {
			update((toasts) => toasts.filter((t) => t.id !== id));
		},
		clear: () => {
			update(() => []);
		},
	};
}

export const toastStore = createToastStore();
