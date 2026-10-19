import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { tick } from 'svelte';
import R18DevDumpSection from './R18DevDumpSection.svelte';
import type { DumpStatus } from '$lib/api/types';

type Frame = { job_id: string; message: string; status: string; progress: number; error?: string };
const api = vi.hoisted(() => ({
	getDumpStatus: vi.fn(),
	uploadDump: vi.fn(),
	downloadDump: vi.fn(),
	clearDump: vi.fn(),
}));
const configApi = vi.hoisted(() => ({ getConfig: vi.fn(), request: vi.fn() }));
const ws = vi.hoisted(() => ({
	messages: [] as Frame[],
	subscribers: new Set<(s: { messages: Frame[] }) => void>(),
}));
vi.mock('$lib/api/client', () => ({
	apiClient: {
		r18dev: api,
		config: { getConfig: configApi.getConfig },
		request: configApi.request,
	},
}));
vi.mock('$lib/stores/websocket', () => ({
	websocketStore: {
		subscribe: (fn: (s: { messages: Frame[] }) => void) => {
			ws.subscribers.add(fn);
			fn({ messages: ws.messages });
			return () => ws.subscribers.delete(fn);
		},
		clearMessages: () => {
			ws.messages = [];
			for (const fn of ws.subscribers) fn({ messages: [] });
		},
	},
}));

if (!window.matchMedia) {
	window.matchMedia = (query: string) => ({
		matches: false,
		media: query,
		onchange: null,
		addEventListener() {},
		removeEventListener() {},
		addListener() {},
		removeListener() {},
		dispatchEvent: () => false,
	});
}
if (!Element.prototype.animate) {
	Element.prototype.animate = function () {
		const animation = {
			onfinish: null as (() => void) | null,
			oncancel: null,
			effect: null,
			playState: 'finished',
			currentTime: 0,
			cancel() {},
			finish() {
				animation.onfinish?.();
			},
			addEventListener() {},
			removeEventListener() {},
		};
		queueMicrotask(() => animation.onfinish?.());
		return animation as unknown as Animation;
	};
}

const status = (overrides: Partial<DumpStatus> = {}): DumpStatus => ({
	present: false,
	enabled: true,
	running: false,
	path: '/synthetic/dump.db',
	...overrides,
});
const oldFailure = 'synthetic previous upload failed';
const errorFrame = (error: string): Frame => ({
	job_id: 'r18dev-dump-download',
	message: 'error',
	status: 'error',
	progress: 0,
	error,
});
function deferred<T>() {
	let resolve!: (value: T) => void;
	let reject!: (reason: Error) => void;
	const promise = new Promise<T>((yes, no) => {
		resolve = yes;
		reject = no;
	});
	return { promise, resolve, reject };
}
async function settle() {
	await act(async () => {
		await Promise.resolve();
		await tick();
	});
}
async function mountSection() {
	const view = render(R18DevDumpSection);
	await settle();
	await fireEvent.click(document.querySelector('button[aria-expanded]') as HTMLElement);
	await settle();
	return view;
}
async function chooseUpload() {
	await fireEvent.change(document.querySelector('input[type="file"]') as HTMLInputElement, {
		target: { files: [new File(['synthetic'], 'synthetic.db')] },
	});
	await settle();
}
async function advancePoll() {
	await act(async () => {
		await vi.advanceTimersByTimeAsync(3000);
	});
}
async function queuedRefreshes(button: HTMLElement) {
	// Queue both events before the loading render removes the button.
	await act(() => {
		button.click();
		button.click();
	});
	await settle();
}

describe('R18DevDumpSection reopened status', () => {
	beforeEach(() => {
		vi.useFakeTimers();
		vi.resetAllMocks();
		ws.messages = [];
		ws.subscribers.clear();
		api.getDumpStatus.mockResolvedValue(status());
		api.uploadDump.mockResolvedValue({ message: 'upload staged' });
		api.downloadDump.mockResolvedValue(undefined);
		api.clearDump.mockResolvedValue(undefined);
		configApi.getConfig.mockResolvedValue({ metadata: {} });
		configApi.request.mockResolvedValue({});
	});
	afterEach(() => {
		cleanup();
		vi.clearAllTimers();
		vi.useRealTimers();
	});

	it.each([
		{ present: false, kind: 'validation' },
		{ present: true, kind: 'validation' },
		{ present: false, kind: 'import' },
		{ present: true, kind: 'reload' },
		{ present: false, kind: undefined },
	])(
		'restores a completed failure on mount: present=$present kind=$kind',
		async ({ present, kind }) => {
			api.getDumpStatus.mockResolvedValue(
				status({ present, last_error: oldFailure, last_error_kind: kind }),
			);
			await mountSection();
			expect(screen.getByText(kind ? `[${kind}] ${oldFailure}` : oldFailure)).toBeTruthy();
			await advancePoll();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(1);
		},
	);

	it('restores a failure completed after the accepted upload page was unmounted', async () => {
		const first = await mountSection();
		await chooseUpload();
		expect(api.uploadDump).toHaveBeenCalledTimes(1);
		first.unmount();
		api.getDumpStatus.mockResolvedValue(
			status({ last_error: oldFailure, last_error_kind: 'import' }),
		);
		await mountSection();
		expect(screen.getByText(`[import] ${oldFailure}`)).toBeTruthy();
		await advancePoll();
		expect(api.getDumpStatus).toHaveBeenCalledTimes(2);
	});

	it.each([false, true])(
		'leaves successful terminal status free of stale errors: present=%s',
		async (present) => {
			ws.messages = [errorFrame('synthetic stale WebSocket error')];
			api.getDumpStatus.mockResolvedValue(
				status({ present, last_error: '', last_error_kind: 'validation' }),
			);
			await mountSection();
			expect(screen.queryByText(/synthetic stale/)).toBeNull();
			expect(screen.queryByText(/\[validation\]/)).toBeNull();
			await advancePoll();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(1);
		},
	);

	it('ignores an older saved error while a job is running and follows its eventual failure', async () => {
		api.getDumpStatus.mockResolvedValue(
			status({ running: true, last_error: oldFailure, last_error_kind: 'validation' }),
		);
		await mountSection();
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
		await advancePoll();
		expect(api.getDumpStatus).toHaveBeenCalledTimes(2);
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
		api.getDumpStatus.mockResolvedValue(
			status({ last_error: 'synthetic current failure', last_error_kind: 'staging' }),
		);
		await advancePoll();
		expect(screen.getByText('[staging] synthetic current failure')).toBeTruthy();
	});

	it('keeps the current WebSocket terminal error when its status refresh has no saved error', async () => {
		await mountSection();
		await chooseUpload();
		ws.messages = [errorFrame('synthetic current WebSocket failure')];
		for (const fn of ws.subscribers) fn({ messages: ws.messages });
		await settle();
		await advancePoll();
		expect(screen.getByText('synthetic current WebSocket failure')).toBeTruthy();
		await advancePoll();
		expect(api.getDumpStatus).toHaveBeenCalledTimes(2);
	});

	it('does not resurrect the saved upload failure after Clear succeeds', async () => {
		api.getDumpStatus
			.mockResolvedValueOnce(
				status({ present: true, last_error: oldFailure, last_error_kind: 'validation' }),
			)
			.mockResolvedValue(status({ last_error: oldFailure, last_error_kind: 'validation' }));
		await mountSection();
		await fireEvent.click(screen.getByRole('button', { name: /^Clear Dump$/ }));
		await settle();
		const buttons = screen.getAllByRole('button', { name: /^Clear Dump$/ });
		await fireEvent.click(buttons[buttons.length - 1]);
		await settle();
		expect(api.clearDump).toHaveBeenCalledTimes(1);
		expect(api.getDumpStatus).toHaveBeenCalledTimes(2);
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
	});

	it('clears a restored error when an explicit refresh reports success', async () => {
		api.getDumpStatus
			.mockResolvedValueOnce(
				status({ present: true, last_error: oldFailure, last_error_kind: 'validation' }),
			)
			.mockResolvedValue(status({ present: true }));
		await mountSection();
		await fireEvent.click(screen.getByRole('button', { name: /^Refresh$/ }));
		await settle();
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
	});

	it('restores a newly completed server failure on an explicit refresh', async () => {
		api.getDumpStatus
			.mockResolvedValueOnce(status({ present: true }))
			.mockResolvedValue(
				status({ present: true, last_error: oldFailure, last_error_kind: 'reload' }),
			);
		await mountSection();
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
		await fireEvent.click(screen.getByRole('button', { name: /^Refresh$/ }));
		await settle();
		expect(screen.getByText(`[reload] ${oldFailure}`)).toBeTruthy();
	});

	it('preserves a newer synchronous upload failure instead of reusing the saved error', async () => {
		api.getDumpStatus.mockResolvedValue(
			status({ last_error: oldFailure, last_error_kind: 'validation' }),
		);
		api.uploadDump.mockRejectedValue(new Error('synthetic request rejected'));
		await mountSection();
		await chooseUpload();
		expect(screen.getByText('synthetic request rejected')).toBeTruthy();
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
		expect(api.getDumpStatus).toHaveBeenCalledTimes(1);
	});

	it.each(['resolve', 'reject'])(
		'freshness: a newer Refresh wins over an older %s',
		async (outcome) => {
			const older = deferred<DumpStatus>();
			api.getDumpStatus
				.mockResolvedValueOnce(status({ present: true }))
				.mockReturnValueOnce(older.promise)
				.mockResolvedValueOnce(
					status({
						present: true,
						enabled: false,
						last_error: 'new Refresh result',
						last_error_kind: 'reload',
					}),
				);
			await mountSection();
			const button = screen.getByRole('button', { name: /^Refresh$/ });
			await queuedRefreshes(button);
			expect(api.getDumpStatus).toHaveBeenCalledTimes(3);
			if (outcome === 'resolve') older.resolve(status({ present: true, last_error: oldFailure }));
			else older.reject(new Error('obsolete status request failed'));
			await settle();
			expect(screen.getByText('[reload] new Refresh result')).toBeTruthy();
			expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('false');
			expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
			expect(screen.queryByText('obsolete status request failed')).toBeNull();
		},
	);

	it('freshness: an older response cannot end the newer Refresh loading state', async () => {
		const older = deferred<DumpStatus>();
		const newer = deferred<DumpStatus>();
		api.getDumpStatus
			.mockResolvedValueOnce(status({ present: true }))
			.mockReturnValueOnce(older.promise)
			.mockReturnValueOnce(newer.promise);
		await mountSection();
		const button = screen.getByRole('button', { name: /^Refresh$/ });
		await queuedRefreshes(button);
		older.resolve(status({ present: true, last_error: oldFailure }));
		await settle();
		expect(screen.queryByRole('button', { name: /^Refresh$/ })).toBeNull();
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
		newer.resolve(status({ present: true }));
		await settle();
		expect(screen.getByRole('button', { name: /^Refresh$/ })).toBeTruthy();
	});

	it('freshness: Clear completion wins over an older explicit Refresh response', async () => {
		const clear = deferred<void>();
		const older = deferred<DumpStatus>();
		api.clearDump.mockReturnValueOnce(clear.promise);
		api.getDumpStatus
			.mockResolvedValueOnce(status({ present: true }))
			.mockReturnValueOnce(older.promise)
			.mockResolvedValue(status({ last_error: oldFailure }));
		await mountSection();
		await fireEvent.click(screen.getByRole('button', { name: /^Clear Dump$/ }));
		const buttons = screen.getAllByRole('button', { name: /^Clear Dump$/ });
		await fireEvent.click(buttons[buttons.length - 1]);
		await settle();
		await fireEvent.click(screen.getByRole('button', { name: /^Refresh$/ }));
		await settle();
		clear.resolve();
		await settle();
		expect(api.getDumpStatus).toHaveBeenCalledTimes(3);
		older.resolve(status({ present: true, last_error: oldFailure }));
		await settle();
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
		expect(screen.queryByText('Dump is present and ready')).toBeNull();
	});

	it.each(['WebSocket', 'poll'])(
		'freshness: %s completion wins over an older config status response',
		async (completion) => {
			const older = deferred<DumpStatus>();
			api.getDumpStatus
				.mockResolvedValueOnce(status({ present: true }))
				.mockReturnValueOnce(older.promise)
				.mockResolvedValue(
					status({ present: true, last_error: 'new completion result', last_error_kind: 'reload' }),
				);
			await mountSection();
			await chooseUpload();
			await fireEvent.click(screen.getByRole('switch'));
			await settle();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(2);
			if (completion === 'WebSocket') {
				ws.messages = [errorFrame('new completion result')];
				for (const fn of ws.subscribers) fn({ messages: ws.messages });
				await settle();
			}
			await advancePoll();
			expect(screen.getByText('[reload] new completion result')).toBeTruthy();
			older.resolve(status({ present: true, running: true, last_error: oldFailure }));
			await settle();
			expect(screen.getByText('[reload] new completion result')).toBeTruthy();
			await advancePoll();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(3);
		},
	);

	it('freshness: a late running response cannot clear a newer config failure or restart polling', async () => {
		const older = deferred<DumpStatus>();
		api.getDumpStatus
			.mockResolvedValueOnce(status({ present: true }))
			.mockReturnValueOnce(older.promise);
		await mountSection();
		await fireEvent.click(screen.getByRole('button', { name: /^Refresh$/ }));
		configApi.request.mockRejectedValueOnce(new Error('new configuration failure'));
		await fireEvent.click(screen.getByRole('switch'));
		await settle();
		older.resolve(status({ present: true, running: true, last_error: oldFailure }));
		await settle();
		expect(screen.getByText('new configuration failure')).toBeTruthy();
		await advancePoll();
		expect(api.getDumpStatus).toHaveBeenCalledTimes(2);
	});

	it('freshness: a newer config status owns enabled metadata as well as its diagnostic', async () => {
		const older = deferred<DumpStatus>();
		api.getDumpStatus
			.mockReturnValueOnce(older.promise)
			.mockResolvedValue(status({ present: true, enabled: false }));
		await mountSection();
		await fireEvent.click(screen.getByRole('switch'));
		await settle();
		older.resolve(status({ present: true, enabled: true, last_error: oldFailure }));
		await settle();
		expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('false');
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
	});

	it('freshness: an unmounted component cannot resume polling from a late status response', async () => {
		const older = deferred<DumpStatus>();
		api.getDumpStatus.mockReturnValueOnce(older.promise).mockResolvedValue(status());
		const view = await mountSection();
		view.unmount();
		older.resolve(status({ running: true }));
		await settle();
		await advancePoll();
		expect(api.getDumpStatus).toHaveBeenCalledTimes(1);
	});

	it('freshness: an obsolete mount response cannot change an in-flight toggle intent', async () => {
		const older = deferred<DumpStatus>();
		const config = deferred<{ metadata: Record<string, unknown> }>();
		api.getDumpStatus
			.mockReturnValueOnce(older.promise)
			.mockResolvedValue(status({ enabled: false }));
		configApi.getConfig.mockReturnValueOnce(config.promise);
		await mountSection();
		await fireEvent.click(screen.getByRole('switch'));
		await settle();
		older.resolve(status({ enabled: true, last_error: oldFailure }));
		await settle();
		expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('false');
		config.resolve({ metadata: {} });
		await settle();
		expect(configApi.request).toHaveBeenCalledTimes(1);
		const payload = JSON.parse(configApi.request.mock.calls[0][1].body);
		expect(payload.metadata.r18dev_dump.enabled).toBe(false);
		expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
	});

	it('freshness: an obsolete mount rejection cannot hide a newer configuration error', async () => {
		const older = deferred<DumpStatus>();
		api.getDumpStatus.mockReturnValueOnce(older.promise);
		configApi.request.mockRejectedValueOnce(new Error('new configuration failure'));
		await mountSection();
		await fireEvent.click(screen.getByRole('switch'));
		await settle();
		older.reject(new Error('obsolete mount status failure'));
		await settle();
		expect(screen.getByText('new configuration failure')).toBeTruthy();
		expect(screen.queryByText('obsolete mount status failure')).toBeNull();
	});

	it.each([true, false])(
		'freshness: a newer config outcome supersedes an in-flight poll: success=%s',
		async (success) => {
			const older = deferred<DumpStatus>();
			api.getDumpStatus
				.mockResolvedValueOnce(status({ present: true, running: true }))
				.mockReturnValueOnce(older.promise);
			if (success) {
				api.getDumpStatus
					.mockResolvedValueOnce(status({ present: true, running: true, enabled: false }))
					.mockResolvedValue(
						status({
							present: true,
							last_error: 'fresh polled failure',
							last_error_kind: 'staging',
						}),
					);
			} else {
				configApi.request.mockRejectedValueOnce(new Error('new configuration failure'));
				api.getDumpStatus.mockResolvedValue(status({ present: true }));
			}
			await mountSection();
			await advancePoll();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(2);
			await fireEvent.click(screen.getByRole('switch'));
			await settle();
			older.resolve(status({ present: true, last_error: oldFailure, last_error_kind: 'import' }));
			await settle();
			expect(screen.queryByText(new RegExp(oldFailure))).toBeNull();
			await advancePoll();
			if (success) expect(screen.getByText('[staging] fresh polled failure')).toBeTruthy();
			else expect(screen.getByText('new configuration failure')).toBeTruthy();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(success ? 4 : 3);
		},
	);

	it.each(['upload', 'download'])(
		'a fresh mount observes the job while an old %s acknowledgement arrives late',
		async (operation) => {
			const ack = deferred<void>();
			let current = status({ present: true });
			api.getDumpStatus.mockImplementation(() => Promise.resolve(current));
			if (operation === 'upload') api.uploadDump.mockReturnValueOnce(ack.promise);
			else api.downloadDump.mockReturnValueOnce(ack.promise);
			const first = await mountSection();
			if (operation === 'upload') await chooseUpload();
			else {
				await fireEvent.click(screen.getByRole('button', { name: /Check for Updates/i }));
				await settle();
			}
			first.unmount();
			current = status({ present: true, running: true });
			await mountSection();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(2);
			ack.resolve();
			await settle();
			await advancePoll();
			await advancePoll();
			await advancePoll();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(5);
			current = status({
				present: true,
				last_error: 'fresh mount observed failure',
				last_error_kind: 'import',
			});
			await advancePoll();
			expect(screen.getByText('[import] fresh mount observed failure')).toBeTruthy();
			expect(api.getDumpStatus).toHaveBeenCalledTimes(6);
		},
	);
});
