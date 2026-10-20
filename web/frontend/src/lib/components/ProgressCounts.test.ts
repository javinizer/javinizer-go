import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, waitFor, within } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { writable } from 'svelte/store';
import type { BatchJobResponse, ProgressMessage } from '$lib/api/types';
import QueryClientWrapper from './QueryClientWrapper.svelte';
import ProgressModal from './ProgressModal.svelte';
import BackgroundJobIndicator from './BackgroundJobIndicator.svelte';
import HomePage from '../../routes/+page.svelte';

// Icons are presentational only; avoid compiling the entire icon catalog here.
vi.mock('lucide-svelte', () => ({
	Activity: () => {},
	ArrowRight: () => {},
	BookOpen: () => {},
	CheckCircle2: () => {},
	Clock3: () => {},
	FolderOpen: () => {},
	History: () => {},
	Link2: () => {},
	RefreshCw: () => {},
	Settings: () => {},
	TriangleAlert: () => {},
	Users: () => {},
	CircleX: () => {},
	LoaderCircle: () => {},
	X: () => {},
	ChevronUp: () => {},
	ChevronDown: () => {},
	Check: () => {},
	XCircle: () => {},
	Ban: () => {},
	FolderInput: () => {},
	CircleCheckBig: () => {},
	ChevronRight: () => {},
}));

// Feed the real rendered consumers a controlled store snapshot. Neither the
// authoritative-count selector nor the production progress arithmetic is mocked.
vi.mock('$lib/stores/websocket', async () => {
	const { writable } = await import('svelte/store');
	return {
		websocketStore: writable({
			connected: false,
			skipped: false,
			messages: [],
			messagesByFile: {},
		}),
	};
});

interface Snapshot {
	connected: boolean;
	skipped: boolean;
	messages: ProgressMessage[];
	messagesByFile: Record<string, Record<string, ProgressMessage>>;
}

const { websocketStore } = await import('$lib/stores/websocket');
const feed = websocketStore as unknown as ReturnType<typeof writable<Snapshot>>;
const clients: QueryClient[] = [];

function message(overrides: Partial<ProgressMessage> = {}): ProgressMessage {
	return {
		job_id: 'job-a',
		file_index: 0,
		file_path: '',
		status: 'running',
		progress: 99,
		message: 'Working',
		...overrides,
	};
}

function job(overrides: Partial<BatchJobResponse> = {}): BatchJobResponse {
	return {
		id: 'job-a',
		status: 'running',
		total_files: 4,
		completed: 1,
		failed: 0,
		progress: 73,
		operation_count: 0,
		reverted_count: 0,
		noop_count: 0,
		excluded: {},
		destination: '',
		results: {},
		started_at: '',
		update: false,
		...overrides,
	};
}

function publish(
	messages: ProgressMessage[] = [],
	messagesByFile: Snapshot['messagesByFile'] = {},
	connected = true,
) {
	feed.set({ messages, messagesByFile, connected, skipped: false });
}

function renderConsumers(rest = job()) {
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false, enabled: false } },
	});
	clients.push(client);
	client.setQueryData(['batch-job-slim', rest.id], rest);
	client.setQueryData(['config'], {});
	client.setQueryData(['health'], { status: 'ok', version: 'test', scrapers: [] });
	client.setQueryData(['history', 'stats'], { total: 0 });
	client.setQueryData(['history', 'recent'], { records: [] });
	client.setQueryData(['history', 'window'], { records: [] });
	client.setQueryData(['actresses', 'count'], { total: 0 });
	client.setQueryData(['cwd'], { path: '' });
	const wrapper = { wrapper: QueryClientWrapper, wrapperProps: { client } };
	const modal = render(ProgressModal, { jobId: rest.id, onClose: vi.fn() }, wrapper);
	const indicator = render(
		BackgroundJobIndicator,
		{ jobId: rest.id, onReopen: vi.fn(), onDismiss: vi.fn() },
		wrapper,
	);
	const home = render(HomePage, {}, wrapper);
	// The modal deliberately portals outside its render container.
	const modalContent = modal.getByText('Overall Progress').parentElement!.parentElement!;
	const activity = home.getByText('Current Activity').parentElement!.parentElement!;
	return { modal: within(modalContent), modalContent, indicator, activity };
}

async function expectProgress(
	views: ReturnType<typeof renderConsumers>,
	finished: number,
	total: number,
	modal: number,
	indicator = modal,
	home: number | null = modal,
) {
	const expand = within(views.indicator.container).queryByRole('button', {
		name: 'Expand',
	});
	if (expand) await fireEvent.click(expand);
	await waitFor(() => {
		expect(views.modal.getByText(`${finished} / ${total} files`)).toBeTruthy();
		expect(views.modal.getByText(`${modal.toFixed(1)}%`)).toBeTruthy();
		expect(views.modalContent.querySelector<HTMLElement>('[style*="width"]')?.style.width).toBe(
			`${modal}%`,
		);
		expect(views.indicator.getByText(`${finished} / ${total} files · ${indicator}%`)).toBeTruthy();
		expect(
			views.indicator.container.querySelector<HTMLElement>('[style*="width"]')?.style.width,
		).toBe(`${indicator}%`);
		if (home === null) {
			expect(within(views.activity).getByText('No recent job activity yet.')).toBeTruthy();
		} else {
			expect(within(views.activity).getByText(`${home}%`)).toBeTruthy();
			expect(views.activity.querySelector<HTMLElement>('[style*="width"]')?.style.width).toBe(
				`${home}%`,
			);
		}
	});
}

beforeEach(() => {
	publish([], {}, false);
	// Guard against accidental live requests: all REST fixtures live in the real
	// query cache, and these tests must not contact a backend or scraper provider.
	vi.stubGlobal(
		'fetch',
		vi.fn(() => Promise.reject(new Error('Unexpected network request'))),
	);
	vi.stubGlobal(
		'WebSocket',
		vi.fn(() => {
			throw new Error('Unexpected WebSocket connection');
		}),
	);
	// jsdom lacks the animation API used by Svelte transitions.
	if (!Element.prototype.animate) {
		Object.defineProperty(Element.prototype, 'animate', {
			value: () => {},
			configurable: true,
			writable: true,
		});
	}
	vi.spyOn(Element.prototype, 'animate').mockImplementation(() => {
		const animation = {
			onfinish: null as (() => void) | null,
			cancel() {},
			finished: Promise.resolve(),
		};
		queueMicrotask(() => animation.onfinish?.());
		return animation as unknown as Animation;
	});
});

afterEach(() => {
	cleanup();
	clients.splice(0).forEach((client) => client.clear());
	expect(fetch).not.toHaveBeenCalled();
	expect(WebSocket).not.toHaveBeenCalled();
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
});

describe('authoritative counts in the real Home, modal, and indicator', () => {
	it('uses newest valid same-job counts past mixed-job and count-less frames', async () => {
		const active = message({ file_path: '/a.mkv', progress: 50 });
		publish(
			[
				message({ total_files: 10, completed: 1, failed: 0 }),
				message({ total_files: 10, completed: 2, failed: 1 }),
				message({ job_id: 'other', total_files: 100, completed: 99 }),
				message({ total_files: 0, completed: 9 }),
				message({ total_files: -2, completed: 9 }),
				message({ total_files: '20' as unknown as number, completed: 9 }),
				active,
			],
			{ 'job-a': { '/a.mkv': active } },
		);
		await expectProgress(renderConsumers(), 3, 10, 35);
	});

	it('accepts aggregate-only counts without inventing totals from per-file rows', async () => {
		publish([message({ total_files: 10, completed: 2, failed: 1 })]);
		await expectProgress(renderConsumers(), 3, 10, 30);
	});

	it.each([
		{ completed: 0, failed: 0 },
		{ completed: undefined, failed: undefined },
		{ completed: 0, failed: undefined },
		{ completed: undefined, failed: 0 },
	])('defaults missing counts to zero rather than REST: %j', async (counts) => {
		publish([message({ total_files: 10, ...counts })]);
		await expectProgress(renderConsumers(job({ completed: 2, failed: 1 })), 0, 10, 0);
	});

	it('uses the latest activity job on Home, not the open modal job', async () => {
		publish([
			message({ total_files: 10, completed: 2, failed: 1 }),
			message({ job_id: 'other', total_files: 20, completed: 12 }),
		]);
		await expectProgress(renderConsumers(), 3, 10, 30, 30, 60);
	});

	it('keeps the indicator REST percentage but the modal count-based percentage without WS', async () => {
		await expectProgress(renderConsumers(), 1, 4, 25, 73, null);
	});

	it('keeps Home early-scrape fallback separate from REST-backed consumers', async () => {
		const active = message({ file_path: '/a.mkv', progress: 50 });
		publish([active], { 'job-a': { '/a.mkv': active } });
		await expectProgress(renderConsumers(), 1, 4, 38, 38, 50);
	});

	it('falls back to the aggregate progress before any totals or file rows arrive', async () => {
		publish([message({ progress: 17 })]);
		await expectProgress(renderConsumers(), 1, 4, 25, 73, 17);
	});

	it('preserves modal REST running-state policy when WS shows an active file', async () => {
		const active = message({ file_path: '/a.mkv', progress: 50, total_files: 10, completed: 2 });
		publish([active], { 'job-a': { '/a.mkv': active } });
		await expectProgress(renderConsumers(job({ status: 'pending' })), 2, 10, 20, 25, 25);
	});

	it('uses REST progress when REST and WS both lack a positive total', async () => {
		publish([message({ total_files: 0, progress: 17 })]);
		await expectProgress(renderConsumers(job({ total_files: 0 })), 1, 0, 73, 73, 17);
	});

	it('reacts to history clearing and reconnect snapshots without retaining stale counts', async () => {
		const counts = message({ total_files: 10, completed: 2, failed: 1 });
		publish([counts]);
		const views = renderConsumers();
		await expectProgress(views, 3, 10, 30);
		// A disconnected socket does not invalidate the retained WS snapshot.
		publish([counts], {}, false);
		await expectProgress(views, 3, 10, 30);
		publish([], {}, false);
		await expectProgress(views, 1, 4, 25, 73, null);
		publish([message({ total_files: 8, completed: 4 })]);
		await expectProgress(views, 4, 8, 50);
	});
});
