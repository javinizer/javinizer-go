import { cleanup, fireEvent, render } from '@testing-library/svelte';
import { beforeEach, afterEach, it, expect, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import { alertDialog } from '$lib/stores/dialog.svelte';
import ActressEditor from './ActressEditor.svelte';
import * as m from '$lib/paraglide/messages';
vi.mock('$lib/query/queries', () => ({
	createConfigQuery: () => ({ data: { output: {}, metadata: {} } }),
}));
vi.mock('$lib/stores/dialog.svelte', () => ({ alertDialog: vi.fn(), confirmDialog: vi.fn() }));
beforeEach(() => {
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
	Object.defineProperty(Element.prototype, 'getAnimations', {
		configurable: true,
		value: () => [],
	});
	vi.spyOn(apiClient, 'request').mockResolvedValue([]);
	vi.spyOn(apiClient.actresses, 'getAliasGroup').mockResolvedValue({ canonical: '', names: [] });
});
afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
	vi.clearAllMocks();
});
const actor = { id: 71, first_name: 'Alice', last_name: 'Example', japanese_name: '日本名' };
const movie = {
	id: 'movie-normal',
	title: 'Normal',
	cast_version: 'v1',
	actresses: [actor],
	credits: [{ id: 41, actress_id: 71, override_name: 'Local name', user_override: true }],
};
async function input(
	view: { getByLabelText: (label: string) => HTMLElement },
	label: string,
	value: string,
) {
	await fireEvent.input(view.getByLabelText(label), { target: { value } });
}
it.each([
	['absent names', { id: 71, dmm_id: 123 }],
	['unnamed', { id: 71, first_name: '', last_name: '', japanese_name: '' }],
	['DMM-only', { id: 71, dmm_id: 123, first_name: '', last_name: '', japanese_name: '' }],
	['surname-only', { id: 71, first_name: '', last_name: 'Surname', japanese_name: '' }],
	['first-name control', { id: 71, first_name: 'First', last_name: '', japanese_name: '' }],
	['Japanese-name control', { id: 71, first_name: '', last_name: '', japanese_name: '名前' }],
])('permits thumbnail-only save without changing valid %s identity', async (_, actor) => {
	const onUpdate = vi.fn(),
		onPersistEdits = vi.fn();
	const view = render(ActressEditor, {
		movie: { ...movie, actresses: [actor] },
		onUpdate,
		onPersistEdits,
	});
	await fireEvent.click(view.getByRole('button', { name: 'Edit Actress' }));
	await input(view, 'Thumbnail URL', 'https://example.test/new-thumb.jpg');
	await fireEvent.click(view.getByRole('button', { name: 'Save Changes' }));
	expect(alertDialog).not.toHaveBeenCalled();
	expect(onUpdate).toHaveBeenCalledOnce();
	expect(onPersistEdits).toHaveBeenCalledOnce();
	expect(onUpdate.mock.lastCall![0].actresses).toEqual([
		{ ...actor, thumb_url: 'https://example.test/new-thumb.jpg', thumb_edited: true },
	]);
	expect(onUpdate.mock.lastCall![0].credits).toEqual(movie.credits);
});
it('keeps creation rule for a new last-only identity', async () => {
	const onUpdate = vi.fn(),
		onPersistEdits = vi.fn();
	const view = render(ActressEditor, {
		movie: { ...movie, actresses: [], credits: [] },
		onUpdate,
		onPersistEdits,
	});
	await fireEvent.click(view.getAllByRole('button', { name: 'Add Actress' })[0]);
	await input(view, 'Last Name', 'Surname');
	const buttons = view.getAllByRole('button', { name: 'Add Actress' });
	await fireEvent.click(buttons[buttons.length - 1]);
	expect(alertDialog).toHaveBeenCalledWith(m.editor_validation_title(), m.editor_validation_body());
	expect(onUpdate).not.toHaveBeenCalled();
	expect(onPersistEdits).not.toHaveBeenCalled();
});
it('rejects clearing every name of an existing named identity', async () => {
	const onUpdate = vi.fn(),
		onPersistEdits = vi.fn();
	const view = render(ActressEditor, { movie, onUpdate, onPersistEdits });
	await fireEvent.click(view.getByRole('button', { name: 'Edit Actress' }));
	for (const label of ['First Name', 'Last Name', 'Japanese Name']) await input(view, label, '');
	await fireEvent.click(view.getByRole('button', { name: 'Save Changes' }));
	expect(alertDialog).toHaveBeenCalledWith(m.editor_validation_title(), m.editor_validation_body());
	expect(onUpdate).not.toHaveBeenCalled();
	expect(onPersistEdits).not.toHaveBeenCalled();
});
it.each([undefined, 0, -1])(
	'rejects thumbnail-only edits of an unpersisted row (%s)',
	async (id) => {
		const onUpdate = vi.fn();
		const view = render(ActressEditor, {
			movie: {
				...movie,
				actresses: [{ id, first_name: '', last_name: 'Surname', japanese_name: '' }],
			},
			onUpdate,
		});
		await fireEvent.click(view.getByRole('button', { name: 'Edit Actress' }));
		await input(view, 'Thumbnail URL', 'https://example.test/new.jpg');
		await fireEvent.click(view.getByRole('button', { name: 'Save Changes' }));
		expect(alertDialog).toHaveBeenCalledWith(
			m.editor_validation_title(),
			m.editor_validation_body(),
		);
		expect(onUpdate).not.toHaveBeenCalled();
	},
);
it.each(['First Name', 'Last Name', 'Japanese Name'])(
	'does not exempt changed %s from the name rule',
	async (label) => {
		const actor = { id: 71, first_name: ' ', last_name: 'Surname', japanese_name: ' ' };
		const onUpdate = vi.fn();
		const view = render(ActressEditor, { movie: { ...movie, actresses: [actor] }, onUpdate });
		await fireEvent.click(view.getByRole('button', { name: 'Edit Actress' }));
		await input(view, label, '');
		await fireEvent.click(view.getByRole('button', { name: 'Save Changes' }));
		expect(alertDialog).toHaveBeenCalledWith(
			m.editor_validation_title(),
			m.editor_validation_body(),
		);
		expect(onUpdate).not.toHaveBeenCalled();
		expect(actor).toEqual({ id: 71, first_name: ' ', last_name: 'Surname', japanese_name: ' ' });
	},
);
it('does not exempt a replacement positive ID selected from the catalog', async () => {
	const actor = { id: 71, first_name: '', last_name: 'Surname', japanese_name: '' };
	vi.mocked(apiClient.request).mockResolvedValue([{ ...actor, id: 72 }]);
	const onUpdate = vi.fn();
	const view = render(ActressEditor, { movie: { ...movie, actresses: [actor] }, onUpdate });
	await fireEvent.click(view.getByRole('button', { name: 'Edit Actress' }));
	await fireEvent.focus(view.getByPlaceholderText(/Type to search actresses/));
	await fireEvent.click(await view.findByRole('button', { name: /Surname/ }));
	await input(view, 'Thumbnail URL', 'https://example.test/new.jpg');
	await fireEvent.click(view.getByRole('button', { name: 'Save Changes' }));
	expect(alertDialog).toHaveBeenCalledWith(m.editor_validation_title(), m.editor_validation_body());
	expect(onUpdate).not.toHaveBeenCalled();
});
it('clears the existing baseline when adding after an edit', async () => {
	const actor = { id: 71, first_name: '', last_name: 'Surname', japanese_name: '' };
	vi.mocked(apiClient.request).mockResolvedValue([actor]);
	const onUpdate = vi.fn();
	const view = render(ActressEditor, { movie: { ...movie, actresses: [actor] }, onUpdate });
	await fireEvent.click(view.getByRole('button', { name: 'Edit Actress' }));
	await fireEvent.click(view.getByRole('button', { name: 'Cancel' }));
	await fireEvent.click(view.getAllByRole('button', { name: 'Add Actress' })[0]);
	await fireEvent.focus(view.getByPlaceholderText(/Type to search actresses/));
	await fireEvent.click(await view.findByRole('button', { name: /Surname/ }));
	await fireEvent.click(view.getAllByRole('button', { name: 'Add Actress' }).at(-1)!);
	expect(alertDialog).toHaveBeenCalledWith(m.editor_validation_title(), m.editor_validation_body());
	expect(onUpdate).not.toHaveBeenCalled();
});
it.each(['First Name', 'Japanese Name'])('allows normal %s edits', async (label) => {
	const onUpdate = vi.fn();
	const view = render(ActressEditor, { movie, onUpdate });
	await fireEvent.click(view.getByRole('button', { name: 'Edit Actress' }));
	await input(view, label, 'Updated');
	await fireEvent.click(view.getByRole('button', { name: 'Save Changes' }));
	expect(alertDialog).not.toHaveBeenCalled();
	expect(onUpdate).toHaveBeenCalledOnce();
});
