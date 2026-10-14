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
		// jsdom has no Web Animations API: the cast grid's flip animation probes
		// element.getAnimations() when a keyed item is replaced.
		Object.defineProperty(Element.prototype, 'getAnimations', {
			configurable: true,
			value: () => [],
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
	it('refreshes authoritative movie state after persisting an override without creating an edit', async () => {
		const updateOverride = vi.spyOn(apiClient, 'updateCreditOverride').mockResolvedValue();
		const onUpdate = vi.fn();
		const onCreditOverridePersisted = vi.fn().mockResolvedValue(undefined);
		const movie = {
			id: 'movie-sync',
			code: 'MOVIE-SYNC',
			title: 'Movie',
			actresses: [{ id: 9, first_name: 'Shared', last_name: 'Identity' }],
			credits: [{ id: 51, actress_id: 9, override_name: '', user_override: false }],
		};

		const view = render(ActressEditor, { movie, onUpdate, onCreditOverridePersisted });
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'Movie Only' },
		});
		const save = view.getAllByRole('button', { name: 'Set name' });
		await fireEvent.click(save[save.length - 1]);

		await waitFor(() => expect(updateOverride).toHaveBeenCalledWith(51, 'Movie Only', true));
		await waitFor(() => expect(onCreditOverridePersisted).toHaveBeenCalledWith('MOVIE-SYNC'));
		expect(onUpdate).not.toHaveBeenCalled();
		expect(view.getByText('Movie-specific name')).toBeTruthy();

		await view.rerender({
			movie: {
				...movie,
				credits: [{ id: 51, actress_id: 9, override_name: 'Movie Only', user_override: true }],
			},
			onUpdate,
			onCreditOverridePersisted,
		});
		expect(view.getByText('Movie-specific name')).toBeTruthy();
	});

	const creditMovie = () => ({
		id: 'movie-draft',
		title: 'Movie',
		actresses: [{ id: 7, first_name: 'Shared', last_name: 'Identity' }],
		credits: [{ id: 41, actress_id: 7, override_name: '', user_override: false }],
	});

	it.each(['refresh clone', 'scalar edit'])(
		'preserves an open dirty draft across a %s',
		async (change) => {
			const movie = creditMovie();
			const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
			await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
			await fireEvent.input(view.getByLabelText('Name for this movie'), {
				target: { value: 'Unsaved stage name' },
			});
			const next =
				change === 'refresh clone'
					? { ...JSON.parse(JSON.stringify(movie)), poster_url: 'new-poster.jpg' }
					: { ...movie, title: 'Updated scalar title' };
			await view.rerender({ movie: next });
			expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe(
				'Unsaved stage name',
			);
		},
	);

	it.each([false, true])(
		'rebases a server override change while preserving dirty=%s',
		async (dirty) => {
			const movie = creditMovie();
			const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
			await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
			if (dirty) {
				await fireEvent.input(view.getByLabelText('Name for this movie'), {
					target: { value: 'Unsaved stage name' },
				});
			}
			const refreshed = {
				...movie,
				credits: [{ ...movie.credits[0], override_name: 'Server name', user_override: true }],
			};
			await view.rerender({ movie: refreshed });
			expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe(
				dirty ? 'Unsaved stage name' : 'Server name',
			);
			expect(view.getByText('Movie-specific name')).toBeTruthy();
			// Further polling must compare against the newly received baseline.
			await view.rerender({ movie: { ...refreshed, title: 'Updated title' } });
			expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe(
				dirty ? 'Unsaved stage name' : 'Server name',
			);
		},
	);

	it('keeps an acknowledged save across a metadata overlay with pre-save credits', async () => {
		vi.spyOn(apiClient, 'updateCreditOverride').mockResolvedValue();
		const movie = creditMovie();
		const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'Saved name' },
		});
		await fireEvent.click(view.getByRole('button', { name: 'Set name' }));
		await waitFor(() => expect(view.getByText('Movie-specific name')).toBeTruthy());
		await view.rerender({ movie: { ...movie, title: 'Updated title' } });
		expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe(
			'Saved name',
		);
		expect(view.getByText('Movie-specific name')).toBeTruthy();
	});

	it('accepts an authoritative revert to the exact pre-save value', async () => {
		vi.spyOn(apiClient, 'updateCreditOverride').mockResolvedValue();
		const movie = creditMovie();
		const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'Saved name' },
		});
		await fireEvent.click(view.getByRole('button', { name: 'Set name' }));
		await waitFor(() => expect(view.getByText('Movie-specific name')).toBeTruthy());
		// Another client clears the override before this component ever receives
		// a snapshot containing the acknowledged save. Equal values do not mean
		// a newly supplied server snapshot is stale.
		await view.rerender({
			movie: { ...movie, credits: movie.credits.map((credit) => ({ ...credit })) },
		});
		expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe('');
		expect(view.queryByText('Movie-specific name')).toBeNull();
		await view.rerender({
			movie: { ...movie, credits: movie.credits.map((credit) => ({ ...credit })) },
		});
		expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe('');
		expect(view.queryByText('Movie-specific name')).toBeNull();
	});

	it('keeps a failed-save draft and error across unrelated updates, then allows retry', async () => {
		vi.spyOn(console, 'error').mockImplementation(() => {});
		const save = vi
			.spyOn(apiClient, 'updateCreditOverride')
			.mockRejectedValueOnce(new Error('offline'))
			.mockResolvedValue();
		const movie = creditMovie();
		const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'Retry name' },
		});
		await fireEvent.click(view.getByRole('button', { name: 'Set name' }));
		await view.findByRole('alert');
		await view.rerender({ movie: { ...movie, title: 'Updated title' } });
		expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe(
			'Retry name',
		);
		expect(view.getByRole('alert')).toBeTruthy();
		await fireEvent.click(view.getByRole('button', { name: 'Set name' }));
		await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
		await waitFor(() => expect(view.queryByRole('alert')).toBeNull());
	});

	it.each(['id', 'code', 'content_id'] as const)(
		'resets the editor when movie %s changes, even with reused credit IDs',
		async (identity) => {
			const movie = { ...creditMovie(), [identity]: 'movie-A' };
			const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
			await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
			await fireEvent.input(view.getByLabelText('Name for this movie'), {
				target: { value: 'Old movie draft' },
			});
			await view.rerender({ movie: { ...movie, [identity]: 'movie-B' } });
			expect(view.queryByLabelText('Name for this movie')).toBeNull();
			await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
			expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe('');
		},
	);

	it('drops removed credit drafts and starts from the server value when re-added', async () => {
		const movie = creditMovie();
		const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'Removed draft' },
		});
		await view.rerender({ movie: { ...movie, credits: [] } });
		expect(view.queryByLabelText('Name for this movie')).toBeNull();
		await view.rerender({ movie });
		expect(view.queryByLabelText('Name for this movie')).toBeNull();
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe('');
	});

	it('drops the draft when an existing credit is reassigned to another identity', async () => {
		const movie = creditMovie();
		const view = render(ActressEditor, { movie, onUpdate: vi.fn() });
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		await fireEvent.input(view.getByLabelText('Name for this movie'), {
			target: { value: 'Other actress draft' },
		});
		await view.rerender({
			movie: {
				...movie,
				actresses: [{ id: 9, first_name: 'New', last_name: 'Identity' }],
				credits: [
					{
						...movie.credits[0],
						actress_id: 9,
						override_name: 'New identity name',
						user_override: true,
					},
				],
			},
		});
		expect(view.queryByLabelText('Name for this movie')).toBeNull();
		await fireEvent.click(view.getByRole('button', { name: 'This movie only' }));
		expect((view.getByLabelText('Name for this movie') as HTMLInputElement).value).toBe(
			'New identity name',
		);
	});

	it('marks an edited thumbnail as explicit intent in the saved payload', async () => {
		vi.spyOn(apiClient, 'request').mockResolvedValue([]);
		const onUpdate = vi.fn();
		const movie = {
			id: 'movie-thumb',
			title: 'Movie',
			cast_version: 'cast-v9',
			actresses: [
				{
					id: 3,
					first_name: 'Shared',
					last_name: 'Identity',
					thumb_url: 'https://old.test/thumb.jpg',
				},
			],
		};

		const view = render(ActressEditor, { movie, onUpdate });
		await fireEvent.click(view.getAllByRole('button', { name: /Edit Actress/i })[0]);
		const thumb = await view.findByLabelText(/Thumbnail URL/i);
		await fireEvent.input(thumb, { target: { value: 'https://new.test/thumb.jpg' } });
		const save = view.getAllByRole('button', { name: /Save Changes/i });
		await fireEvent.click(save[save.length - 1]);

		await waitFor(() => expect(onUpdate).toHaveBeenCalled());
		const updated = onUpdate.mock.lastCall?.[0];
		expect(updated.actresses[0]).toMatchObject({
			thumb_url: 'https://new.test/thumb.jpg',
			thumb_edited: true,
		});
	});

	it('re-baselines the thumbnail comparison when the search selection changes', async () => {
		vi.spyOn(apiClient, 'request').mockResolvedValue([
			{
				id: 9,
				first_name: 'Other',
				last_name: 'Identity',
				thumb_url: 'https://b.test/thumb.jpg',
			},
		]);
		const onUpdate = vi.fn();
		const movie = {
			id: 'movie-thumb-baseline',
			title: 'Movie',
			actresses: [
				{
					id: 3,
					first_name: 'First',
					last_name: 'Actress',
					thumb_url: 'https://a.test/thumb.jpg',
				},
			],
		};

		const view = render(ActressEditor, { movie, onUpdate });
		// Open the editor on actress A, then pick actress B from the search
		// results: the comparison baseline must follow B, not stay on A's URL.
		await fireEvent.click(view.getAllByRole('button', { name: /Edit Actress/i })[0]);
		await fireEvent.focus(view.getByPlaceholderText(/Type to search actresses/i));
		const row = await view.findByText('Identity Other');
		await fireEvent.click(row);

		// Change B's thumbnail, then restore it to B's original URL.
		const thumb = await view.findByLabelText(/Thumbnail URL/i);
		await fireEvent.input(thumb, { target: { value: 'https://new.test/thumb.jpg' } });
		await fireEvent.input(thumb, { target: { value: 'https://b.test/thumb.jpg' } });
		const save = view.getAllByRole('button', { name: /Save Changes/i });
		await fireEvent.click(save[save.length - 1]);

		await waitFor(() => expect(onUpdate).toHaveBeenCalled());
		const updated = onUpdate.mock.lastCall?.[0];
		expect(updated.actresses[0]).toMatchObject({
			id: 9,
			thumb_url: 'https://b.test/thumb.jpg',
		});
		expect(updated.actresses[0].thumb_edited).toBe(false);
	});
});
