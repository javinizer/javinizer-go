import { cleanup, fireEvent, render } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import type { Actress } from '$lib/api/types';
import { alertDialog } from '$lib/stores/dialog.svelte';
import * as m from '$lib/paraglide/messages';
import ActressEditor from './ActressEditor.svelte';

vi.mock('$lib/query/queries', () => ({
	createConfigQuery: () => ({ data: { output: {}, metadata: {} } }),
}));
vi.mock('$lib/stores/dialog.svelte', () => ({ alertDialog: vi.fn(), confirmDialog: vi.fn() }));

const alice = { id: 71, first_name: 'Alice', last_name: 'Example' };
const betty = { id: 72, first_name: 'Betty', last_name: 'Sample' };
const clara = { id: 73, first_name: 'Clara', last_name: 'Other' };

beforeEach(() => {
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
	Object.defineProperty(Element.prototype, 'getAnimations', {
		configurable: true,
		value: () => [],
	});
	vi.spyOn(apiClient.actresses, 'getAliasGroup').mockResolvedValue({ canonical: '', names: [] });
	vi.spyOn(apiClient, 'request').mockResolvedValue([alice, betty, clara]);
});
afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
	vi.clearAllMocks();
});

function setup(cast: Actress[]) {
	const movie = {
		id: 'movie-test',
		title: 'Test',
		cast_version: 'cast-v7',
		actresses: cast,
		credits: [{ id: 41, actress_id: 71, override_name: 'Movie name', user_override: true }],
	};
	const onUpdate = vi.fn();
	const onPersistEdits = vi.fn();
	const view = render(ActressEditor, { movie, onUpdate, onPersistEdits });
	return { ...view, movie, onUpdate, onPersistEdits };
}

type View = ReturnType<typeof setup>;
async function open(view: View, edit: boolean) {
	await fireEvent.click(
		edit
			? view.getAllByRole('button', { name: 'Edit Actress' })[0]
			: view.getByRole('button', { name: 'Add Actress' }),
	);
}
async function select(view: View, actress: Actress) {
	await fireEvent.focus(view.getByPlaceholderText(/Type to search actresses/));
	await fireEvent.click(
		await view.findByRole('button', {
			name: new RegExp(`${actress.last_name} ${actress.first_name}`),
		}),
	);
}
async function save(view: View, edit: boolean) {
	await fireEvent.click(
		edit
			? view.getByRole('button', { name: 'Save Changes' })
			: view.getAllByRole('button', { name: 'Add Actress' }).at(-1)!,
	);
}

describe('catalog identity validation', () => {
	it.each([false, true])(
		'rejects duplicate selection and lets the draft recover (edit=%s)',
		async (edit) => {
			const view = setup([alice, betty]);
			const original = structuredClone(view.movie);
			await open(view, edit);
			await select(view, betty);
			await fireEvent.input(view.getByLabelText(/Japanese Name/), {
				target: { value: 'Draft name' },
			});
			await save(view, edit);
			expect(alertDialog).toHaveBeenCalledWith(
				m.editor_validation_title(),
				m.editor_duplicate_actress_body(),
			);
			expect(view.onUpdate).not.toHaveBeenCalled();
			expect(view.onPersistEdits).not.toHaveBeenCalled();
			expect(view.movie).toEqual(original);
			expect((view.getByLabelText(/Japanese Name/) as HTMLInputElement).value).toBe('Draft name');
			expect((view.getByLabelText(/First Name/) as HTMLInputElement).value).toBe('Betty');
			expect(view.getByText('Movie-specific name')).toBeTruthy();
			// A repeated submit remains harmless; choosing a distinct identity recovers.
			await save(view, edit);
			expect(view.onPersistEdits).not.toHaveBeenCalled();
			await select(view, clara);
			await save(view, edit);
			expect(view.onUpdate).toHaveBeenCalledOnce();
			expect(view.onPersistEdits).toHaveBeenCalledOnce();
			expect(view.onUpdate.mock.calls[0][0]).toEqual({
				...original,
				actresses: edit ? [clara, betty] : [alice, betty, clara],
			});
		},
	);

	it('allows selecting the edited row itself and preserves movie choices', async () => {
		const view = setup([alice, betty]);
		await open(view, true);
		await select(view, alice);
		await fireEvent.input(view.getByLabelText(/First Name/), { target: { value: 'Updated' } });
		await save(view, true);
		expect(alertDialog).not.toHaveBeenCalled();
		expect(view.onUpdate).toHaveBeenCalledWith({
			...view.movie,
			actresses: [{ ...alice, first_name: 'Updated' }, betty],
		});
		expect(view.onPersistEdits).toHaveBeenCalledOnce();
	});

	it('allows a distinct catalog identity with the same name', async () => {
		const sameName = { ...alice, id: 74 };
		vi.mocked(apiClient.request).mockResolvedValue([sameName]);
		const view = setup([alice]);
		await open(view, false);
		await select(view, sameName);
		await save(view, false);
		expect(alertDialog).not.toHaveBeenCalled();
		expect(view.onUpdate.mock.calls[0][0].actresses).toEqual([alice, sameName]);
		expect(view.onPersistEdits).toHaveBeenCalledOnce();
	});

	it.each([undefined, 0, -1])(
		'allows unsaved identity %s without name-based deduplication',
		async (id) => {
			const unsaved = { ...alice, id };
			vi.mocked(apiClient.request).mockResolvedValue([unsaved]);
			const view = setup([alice]);
			await open(view, false);
			await select(view, unsaved);
			await save(view, false);
			expect(alertDialog).not.toHaveBeenCalled();
			expect(view.onUpdate.mock.calls[0][0].actresses).toEqual([alice, unsaved]);
			expect(view.onPersistEdits).toHaveBeenCalledOnce();
		},
	);
});
