import { fireEvent, render, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import ActressEditor from './ActressEditor.svelte';

vi.mock('$lib/query/queries', () => ({
	createConfigQuery: () => ({ data: { output: {}, metadata: {} } }),
}));

describe('ActressEditor production persistence', () => {
	beforeEach(() => {
		Object.defineProperty(Element.prototype, 'animate', {
			configurable: true,
			value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
		});
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it('emits the authoritative cast generation with metadata when adding an actress', async () => {
		vi.spyOn(apiClient, 'request').mockResolvedValue([]);
		const onUpdate = vi.fn();
		const onPersistEdits = vi.fn();
		const movie = {
			id: 'movie-1',
			title: 'Canonical title',
			cast_version: 'cast-v4',
			cropped_poster_url: 'https://example.test/crop.jpg',
			actresses: [],
		};

		const view = render(ActressEditor, { movie, onUpdate, onPersistEdits });
		await fireEvent.click(view.getAllByRole('button', { name: /Add Actress/i })[0]);
		const firstName = await view.findByLabelText(/First Name/i);
		await fireEvent.input(firstName, { target: { value: 'New' } });
		const addButtons = view.getAllByRole('button', { name: /Add Actress/i });
		await fireEvent.click(addButtons[addButtons.length - 1]);

		await waitFor(() => expect(onUpdate).toHaveBeenCalled());
		const updatedMovie = onUpdate.mock.lastCall?.[0];
		expect(updatedMovie).toMatchObject({
			cast_version: 'cast-v4',
			title: 'Canonical title',
			cropped_poster_url: 'https://example.test/crop.jpg',
		});
		expect(updatedMovie.actresses).toEqual([expect.objectContaining({ first_name: 'New' })]);
		expect(onPersistEdits).toHaveBeenCalledOnce();
	});

	it('sets a movie-specific name with the matched credit id and shows the indicator', async () => {
		const updateOverride = vi.spyOn(apiClient, 'updateCreditOverride').mockResolvedValue();
		const movie = {
			id: 'movie-1',
			title: 'Movie',
			actresses: [{ id: 7, first_name: 'Shared', last_name: 'Identity' }],
			credits: [{ id: 41, actress_id: 7, override_name: '', user_override: false }],
		};

		const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
		expect(view.queryByText('Movie-specific name')).toBeNull();
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'Stage Name' },
		});
		await fireEvent.click(view.getByRole('button', { name: 'Set name' }));

		await waitFor(() => expect(updateOverride).toHaveBeenCalledWith(41, 'Stage Name', true));
		expect(await view.findByText('Movie-specific name')).toBeTruthy();
	});

	it('shows an existing movie override and clears it explicitly', async () => {
		const updateOverride = vi.spyOn(apiClient, 'updateCreditOverride').mockResolvedValue();
		const movie = {
			id: 'movie-1',
			title: 'Movie',
			actresses: [{ id: 7, first_name: 'Shared', last_name: 'Identity' }],
			credits: [{ id: 41, actress_id: 7, override_name: 'Movie Name', user_override: true }],
		};

		const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
		expect(view.getByText('Movie-specific name')).toBeTruthy();
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe(
			'Movie Name',
		);
		await fireEvent.click(view.getByRole('button', { name: 'Clear override' }));

		await waitFor(() => expect(updateOverride).toHaveBeenCalledWith(41, '', false));
		await waitFor(() => expect(view.queryByText('Movie-specific name')).toBeNull());
	});

	it('hides the movie-only control when the actress has no credit id', () => {
		const movie = {
			id: 'movie-1',
			title: 'Movie',
			actresses: [{ id: 7, first_name: 'Shared', last_name: 'Identity' }],
			credits: [{ actress_id: 7 }],
		};

		const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
		expect(view.queryByRole('button', { name: 'This movie only' })).toBeNull();
	});
});
