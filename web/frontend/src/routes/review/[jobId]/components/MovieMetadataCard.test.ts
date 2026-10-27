import { cleanup, fireEvent, render } from '@testing-library/svelte';
import { afterEach, expect, it, vi } from 'vitest';
import type { FileResult } from '$lib/api/types';
import MovieMetadataCard from './MovieMetadataCard.svelte';
vi.mock('$lib/query/queries', () => ({
	createFavoriteGenresQuery: () => ({ data: { favorites: [] } }),
}));
vi.mock('@tanstack/svelte-query', () => ({
	useQueryClient: () => ({ invalidateQueries: vi.fn() }),
	createMutation: () => ({ isPending: false, mutate: vi.fn() }),
}));
vi.mock('$lib/components/MovieEditor.svelte', async () => ({
	default: (await import('$lib/components/ui/Card.svelte')).default,
}));
afterEach(cleanup);
function renderCard(isRescraping = false) {
	const movie = { id: 'EVIDENCE-001', title: 'Original movie' };
	const onOpenRescrape = vi.fn(),
		onOpenSourceViewer = vi.fn(),
		onResetCurrentMovie = vi.fn();
	return {
		...render(MovieMetadataCard, {
			currentMovie: movie,
			currentResult: { movie } as FileResult,
			showFieldScraperSources: false,
			isRescraping,
			onOpenRescrape,
			onOpenSourceViewer,
			onResetCurrentMovie,
			onUpdateCurrentMovie: vi.fn(),
		}),
		onOpenRescrape,
		onOpenSourceViewer,
		onResetCurrentMovie,
	};
}
it('wraps metadata header, options and action buttons rather than overflowing the card', async () => {
	const view = renderCard();
	const row = view.getByRole('heading', { name: 'Movie Metadata' }).parentElement!;
	const buttons = view.getByRole('button', { name: 'Sources' }).parentElement!;
	for (const element of [row, buttons, buttons.parentElement!])
		expect(element.classList.contains('flex-wrap')).toBe(true);
	expect(buttons.parentElement!.classList.contains('min-w-0')).toBe(true);
	expect(view.getByRole('checkbox', { name: 'Show scraper per field' })).toBeTruthy();
	await fireEvent.click(view.getByRole('button', { name: 'Sources' }));
	await fireEvent.click(view.getByRole('button', { name: 'Rescrape' }));
	await fireEvent.click(view.getByRole('button', { name: 'Reset to Original' }));
	expect(view.onOpenSourceViewer).toHaveBeenCalledOnce();
	expect(view.onOpenRescrape).toHaveBeenCalledOnce();
	expect(view.onResetCurrentMovie).toHaveBeenCalledOnce();
});
it('preserves rescrape disabled state while wrapping controls', () => {
	const view = renderCard(true);
	expect((view.getByRole('button', { name: 'Rescraping...' }) as HTMLButtonElement).disabled).toBe(
		true,
	);
});
