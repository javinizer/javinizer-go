import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import R18DevDumpSection from './R18DevDumpSection.svelte';
import type { DumpSearchResult } from '$lib/api/types';

const searchDump = vi.fn();
const uploadDump = vi.fn();

vi.mock('$lib/api/client', () => ({
	apiClient: {
		r18dev: {
			getDumpStatus: vi.fn(),
			searchDump: (...args: unknown[]) => searchDump(...args),
			uploadDump: (...args: unknown[]) => uploadDump(...args),
			downloadDump: vi.fn().mockResolvedValue(undefined),
			clearDump: vi.fn().mockResolvedValue(undefined),
		},
	},
}));

let wsSubscriber: ((s: { messages: unknown[] }) => void) | null = null;

vi.mock('$lib/stores/websocket', () => ({
	websocketStore: {
		subscribe: (fn: (s: { messages: unknown[] }) => void) => {
			wsSubscriber = fn;
			fn({ messages: [] });
			return () => {};
		},
		clearMessages: () => {},
	},
}));

if (!Element.prototype.animate) {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	(Element.prototype as any).animate = function () {
		const anim = {
			onfinish: null as (() => void) | null,
			oncancel: null as (() => void) | null,
			effect: null as unknown,
			playState: 'finished' as const,
			currentTime: 0,
			cancel() {},
			finish() {
				anim.onfinish?.();
			},
			addEventListener() {},
			removeEventListener() {},
		};
		queueMicrotask(() => anim.onfinish?.());
		return anim;
	};
}

const mod = await import('$lib/api/client');
const mockGetDumpStatus = vi.mocked(mod.apiClient.r18dev.getDumpStatus);

async function expandSection() {
	const header = document.querySelector('button[aria-expanded]') as HTMLElement;
	await fireEvent.click(header);
}

const dumpFile = (name = 'r18dotdev_dump_2026-09-20.sql.gz') => new File(['dump-bytes'], name);

describe('R18DevDumpSection upload', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockGetDumpStatus.mockResolvedValue({
			present: false,
			enabled: true,
			running: false,
			path: '/tmp/x.db',
		} as never);
	});

	it('selecting a file calls uploadDump and never triggers the config-change callback', async () => {
		const onConfigChange = vi.fn();
		uploadDump.mockRejectedValue(new Error('stop-here'));
		render(R18DevDumpSection, { props: { onConfigChange } });
		await expandSection();

		const input = document.querySelector('input[type="file"]') as HTMLInputElement;
		expect(input).toBeTruthy();
		expect(input.getAttribute('accept')).toBe('.db,.gz');
		await fireEvent.change(input, { target: { files: [dumpFile()] } });

		await waitFor(() => expect(uploadDump).toHaveBeenCalledTimes(1));
		expect((uploadDump.mock.calls[0][0] as File).name).toBe('r18dotdev_dump_2026-09-20.sql.gz');
		expect(onConfigChange).not.toHaveBeenCalled();
	});

	it('shows the server-provided sync error verbatim', async () => {
		uploadDump.mockRejectedValue(new Error('multipart: no file part'));
		render(R18DevDumpSection);
		await expandSection();

		const input = document.querySelector('input[type="file"]') as HTMLInputElement;
		await fireEvent.change(input, { target: { files: [dumpFile('junk.bin')] } });

		await waitFor(() => expect(screen.getByText('multipart: no file part')).toBeTruthy());
	});

	it('renders the error kind when the terminal frame arrives over WebSocket', async () => {
		// Codex #273: when the WS terminal error frame wins the race, its text
		// must still carry the status endpoint's error kind.
		uploadDump.mockResolvedValue({ message: 'upload staged' });
		mockGetDumpStatus.mockResolvedValue({
			present: false,
			enabled: true,
			running: false,
			last_error: 'invalid dump sidecar: missing required table videos',
			last_error_kind: 'validation',
			path: '/tmp/x.db',
		} as never);

		render(R18DevDumpSection);
		await expandSection();
		const input = document.querySelector('input[type="file"]') as HTMLInputElement;
		await fireEvent.change(input, { target: { files: [dumpFile('bad.db')] } });

		// Deliver the WS terminal error frame while the poll sleeps.
		setTimeout(() => {
			wsSubscriber?.({
				messages: [
					{
						job_id: 'r18dev-dump-download',
						message: 'error',
						status: 'error',
						progress: 0,
						error: 'invalid dump sidecar: missing required table videos',
					},
				],
			});
		}, 300);

		await waitFor(
			() => {
				expect(
					screen.getByText('[validation] invalid dump sidecar: missing required table videos'),
				).toBeTruthy();
			},
			{ timeout: 9000 },
		);
	}, 12_000);

	it('renders async failures with the server error kind prefix', async () => {
		uploadDump.mockResolvedValue({ message: 'upload staged' });
		mockGetDumpStatus.mockResolvedValue({
			present: false,
			enabled: true,
			running: false,
			last_error: 'invalid dump sidecar: missing required column videos.dvd_id_norm',
			last_error_kind: 'validation',
			path: '/tmp/x.db',
		} as never);

		render(R18DevDumpSection);
		await expandSection();
		const input = document.querySelector('input[type="file"]') as HTMLInputElement;
		await fireEvent.change(input, { target: { files: [dumpFile('dump.db')] } });

		await waitFor(
			() => {
				expect(
					screen.getByText(
						'[validation] invalid dump sidecar: missing required column videos.dvd_id_norm',
					),
				).toBeTruthy();
			},
			{ timeout: 8000 },
		);
	});
});

describe('R18DevDumpSection search states', () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockGetDumpStatus.mockResolvedValue({
			present: true,
			enabled: true,
			running: false,
			row_count: 1847688,
			path: '/tmp/x.db',
		} as never);
	});

	it('renders present-without-dvd_id matches instead of "no match"', async () => {
		const result: DumpSearchResult = {
			query: 'LULU-441',
			content_id: null,
			dvd_id: null,
			state: 'no_dvd_id',
			matches: [
				{ content_id: 'lulu00441', release_date: '2026-07-03', service_code: 'digital' },
				{ content_id: 'lulu441', release_date: '2026-07-07', service_code: 'mono' },
			],
		};
		searchDump.mockResolvedValue(result);

		render(R18DevDumpSection);

		const header = document.querySelector('button[aria-expanded]') as HTMLElement;
		await fireEvent.click(header);
		const input = await screen.findByRole('textbox');
		await fireEvent.input(input, { target: { value: 'LULU-441' } });
		await fireEvent.click(screen.getByRole('button', { name: /search/i }));

		await waitFor(() => expect(screen.getByText(/lulu00441/)).toBeTruthy());
		expect(screen.getByText(/lulu441/)).toBeTruthy();
		expect(screen.getByText(/no dvd_id/)).toBeTruthy();
		expect(screen.queryByText(/No match/i)).toBeNull();
	});

	it('renders mapped results with content_id label', async () => {
		searchDump.mockResolvedValue({
			query: 'ABF-030',
			content_id: '118abf030',
			dvd_id: null,
			state: 'mapped',
			matches: [{ content_id: '118abf030', dvd_id: 'ABF-030' }],
		} satisfies DumpSearchResult);

		render(R18DevDumpSection);

		const header = document.querySelector('button[aria-expanded]') as HTMLElement;
		await fireEvent.click(header);
		const input = await screen.findByRole('textbox');
		await fireEvent.input(input, { target: { value: 'ABF-030' } });
		await fireEvent.click(screen.getByRole('button', { name: /search/i }));

		await waitFor(() => expect(screen.getByText('118abf030')).toBeTruthy());
		expect(screen.queryByText(/no dvd_id/)).toBeNull();
	});
});
