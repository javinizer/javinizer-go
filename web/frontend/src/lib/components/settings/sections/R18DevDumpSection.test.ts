import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import R18DevDumpSection from './R18DevDumpSection.svelte';
import type { DumpSearchResult } from '$lib/api/types';

const searchDump = vi.fn();

vi.mock('$lib/api/client', () => ({
	apiClient: {
		r18dev: {
			getDumpStatus: vi.fn(),
			searchDump: (...args: unknown[]) => searchDump(...args),
		},
	},
}));

vi.mock('$lib/stores/websocket', () => ({
	websocketStore: {
		subscribe: (fn: (s: { messages: unknown[] }) => void) => {
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
