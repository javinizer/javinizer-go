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
		expect(updatedMovie.actresses).toEqual([
			expect.objectContaining({ first_name: 'New' }),
		]);
		expect(onPersistEdits).toHaveBeenCalledOnce();
	});
});
