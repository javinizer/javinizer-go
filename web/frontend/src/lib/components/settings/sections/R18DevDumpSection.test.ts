import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { writable } from 'svelte/store';
import type { DumpStatus } from '$lib/api/types';
import R18DevDumpSection from './R18DevDumpSection.svelte';

const { clearMessages, downloadDump, getDumpStatus } = vi.hoisted(() => ({
	clearMessages: vi.fn(),
	downloadDump: vi.fn(),
	getDumpStatus: vi.fn(),
}));

vi.mock('$lib/stores/websocket', () => ({
	websocketStore: {
		subscribe: writable({ connected: true, skipped: false, messages: [], messagesByFile: {} })
			.subscribe,
		clearMessages,
	},
}));

vi.mock('$lib/api/client', () => ({
	apiClient: {
		r18dev: {
			getDumpStatus,
			downloadDump,
		},
	},
}));

if (!Element.prototype.animate) {
	Element.prototype.animate = vi.fn(() => ({
		cancel: vi.fn(),
		commitStyles: vi.fn(),
		currentTime: 0,
		effect: null,
		finish: vi.fn(),
		finished: Promise.resolve(),
		id: '',
		oncancel: null,
		onfinish: null,
		onremove: null,
		pause: vi.fn(),
		pending: false,
		persist: vi.fn(),
		play: vi.fn(),
		playState: 'finished',
		playbackRate: 1,
		ready: Promise.resolve(),
		replaceState: 'active',
		reverse: vi.fn(),
		startTime: 0,
		timeline: null,
		updatePlaybackRate: vi.fn(),
		addEventListener: vi.fn(),
		dispatchEvent: vi.fn(),
		removeEventListener: vi.fn(),
	})) as unknown as typeof Element.prototype.animate;
}

function status(overrides: Partial<DumpStatus> = {}): DumpStatus {
	return {
		present: false,
		running: false,
		path: '/tmp/r18dev.db',
		enabled: true,
		...overrides,
	};
}

beforeEach(() => {
	vi.clearAllMocks();
	getDumpStatus.mockResolvedValue(status());
	downloadDump.mockRejectedValue(new Error('stop polling'));
});

describe('R18DevDumpSection', () => {
	it('clears stale shared dump progress before starting a download', async () => {
		const { container } = render(R18DevDumpSection);
		await waitFor(() => expect(getDumpStatus).toHaveBeenCalled());

		const header = container.querySelector('button[aria-expanded="false"]') as HTMLButtonElement;
		await fireEvent.click(header);
		const button = await waitFor(() => {
			const element = Array.from(container.querySelectorAll('button')).find((candidate) =>
				candidate.textContent?.includes('Download Dump'),
			);
			expect(element).toBeTruthy();
			return element as HTMLButtonElement;
		});
		await fireEvent.click(button);

		expect(clearMessages).toHaveBeenCalledWith('r18dev-dump-download');
		expect(downloadDump).toHaveBeenCalledWith(false);
		expect(clearMessages.mock.invocationCallOrder[0]).toBeLessThan(
			downloadDump.mock.invocationCallOrder[0],
		);
	});
});
