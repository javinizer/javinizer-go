import { render, cleanup } from '@testing-library/svelte';
import { tick } from 'svelte';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import type { Movie } from '$lib/api/types';
import { buildDisplayTitlePreviewSignature } from '$lib/utils/display-title-preview';
import MovieEditor from './MovieEditor.svelte';
import * as m from '$lib/paraglide/messages';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
	cleanup();
	vi.useRealTimers();
	vi.restoreAllMocks();
});

function movieFixture(): Movie {
	return {
		id: 'MIRD-281',
		title: 'Stable title',
		genres: [],
		actresses: [
			{ id: 7, first_name: 'Stable', last_name: 'Name', thumb_url: 'https://example.test/old.jpg' },
		],
		credits: [
			{ actress_id: 7, credited_name: 'Stable Name', render_visible: true, order_index: 0 },
		],
		poster_url: 'https://example.test/old-poster.jpg',
	} as Movie;
}

async function flushPreview(ms = 250) {
	await tick();
	await vi.advanceTimersByTimeAsync(ms);
	await tick();
}

it('does not refetch an unchanged title after metadata-only polling refreshes', async () => {
	const preview = vi
		.spyOn(apiClient, 'previewDisplayTitle')
		.mockResolvedValue({ display_title: 'Stable rendered title' });
	const movie = movieFixture();
	const view = render(MovieEditor, {
		movie,
		originalMovie: movie,
		onUpdate: vi.fn(),
		jobId: 'job-1',
		resultId: 'result-1',
	});
	await flushPreview();
	expect(preview).toHaveBeenCalledTimes(1);
	const refreshed = {
		...movie,
		poster_url: 'https://example.test/new-poster.jpg',
		actresses: movie.actresses!.map((a) => ({ ...a, thumb_url: 'https://example.test/new.jpg' })),
	};
	expect(buildDisplayTitlePreviewSignature(refreshed)).toBe(
		buildDisplayTitlePreviewSignature(movie),
	);
	await view.rerender({ movie: refreshed });
	await flushPreview();
	expect(preview).toHaveBeenCalledTimes(1);
	await view.rerender({ movie: { ...refreshed, title: 'A genuine title edit' } });
	await flushPreview();
	expect(preview).toHaveBeenCalledTimes(2);
});

it('keeps an unavailable preview stable during an irrelevant poll', async () => {
	vi.spyOn(apiClient, 'previewDisplayTitle').mockRejectedValue(new Error('preview unavailable'));
	const movie = movieFixture();
	const view = render(MovieEditor, {
		movie,
		originalMovie: movie,
		onUpdate: vi.fn(),
		jobId: 'job-1',
		resultId: 'result-1',
	});
	await flushPreview();
	expect(view.queryByText(m.movie_display_title_preview_error())).not.toBeNull();
	await view.rerender({ movie: { ...movie, poster_url: 'https://example.test/new.jpg' } });
	await tick();
	expect(view.queryByText(m.movie_display_title_preview_error())).not.toBeNull();
	expect(view.queryByText(m.movie_display_title_preview_loading())).toBeNull();
});

it('does not restart a pending preview debounce after an irrelevant poll', async () => {
	const preview = vi
		.spyOn(apiClient, 'previewDisplayTitle')
		.mockResolvedValue({ display_title: 'Rendered title' });
	const movie = movieFixture();
	const view = render(MovieEditor, {
		movie,
		originalMovie: movie,
		onUpdate: vi.fn(),
		jobId: 'job-1',
		resultId: 'result-1',
	});
	await flushPreview(100);
	await view.rerender({ movie: { ...movie, poster_url: 'https://example.test/new.jpg' } });
	await flushPreview(105);
	expect(preview).toHaveBeenCalledTimes(1);
});

it('clears a previous title and refetches after the result target changes', async () => {
	const preview = vi
		.spyOn(apiClient, 'previewDisplayTitle')
		.mockResolvedValueOnce({ display_title: 'Old result title' })
		.mockResolvedValueOnce({ display_title: 'New result title' });
	const movie = movieFixture();
	const view = render(MovieEditor, {
		movie,
		originalMovie: movie,
		onUpdate: vi.fn(),
		jobId: 'job-1',
		resultId: 'result-1',
	});
	await flushPreview();
	expect(view.queryByText('Old result title')).not.toBeNull();
	await view.rerender({ resultId: 'result-2' });
	await tick();
	expect(view.queryByText('Old result title')).toBeNull();
	await flushPreview();
	expect(preview).toHaveBeenCalledTimes(2);
	expect(preview.mock.lastCall?.[1]).toBe('result-2');
	expect(view.queryByText('New result title')).not.toBeNull();
});

it('ignores a response from a disabled old target', async () => {
	let finish!: (response: { display_title: string }) => void;
	vi.spyOn(apiClient, 'previewDisplayTitle').mockImplementation(
		() =>
			new Promise((resolve) => {
				finish = resolve;
			}),
	);
	const movie = movieFixture();
	const view = render(MovieEditor, {
		movie,
		originalMovie: movie,
		onUpdate: vi.fn(),
		jobId: 'job-1',
		resultId: 'result-1',
	});
	await flushPreview();
	await view.rerender({ jobId: undefined, resultId: undefined });
	await tick();
	finish({ display_title: 'Stale old result' });
	await tick();
	await view.rerender({ jobId: 'job-2', resultId: 'result-2' });
	await tick();
	expect(view.queryByText('Stale old result')).toBeNull();
});
