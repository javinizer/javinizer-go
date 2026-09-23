import { fireEvent, render, waitFor, within } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import type { Actress } from '$lib/api/types';
import ActressCandidatesSection from './ActressCandidatesSection.svelte';

vi.mock('$lib/api/client', () => ({
	apiClient: {
		listCandidates: vi.fn(),
		promoteCandidate: vi.fn(),
	},
}));

const mod = await import('$lib/api/client');
const listCandidates = vi.mocked(mod.apiClient.listCandidates);
const promoteCandidate = vi.mocked(mod.apiClient.promoteCandidate);

function candidate(id: number, values: Partial<Actress> = {}): Actress {
	return { id, verified: false, ...values };
}

function renderSection(candidates: Actress[], total = candidates.length) {
	listCandidates.mockResolvedValue({ candidates, total });
	const client = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	const view = render(
		ActressCandidatesSection,
		{},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	return { ...view, client };
}

async function openCandidates(view: ReturnType<typeof renderSection>) {
	await fireEvent.click(view.getByRole('button', { name: /Candidate identities/i }));
	await view.findByRole('group', { name: /candidate #1/i });
}

beforeEach(() => {
	vi.clearAllMocks();
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		value: () => ({ cancel: vi.fn(), finished: Promise.resolve(), onfinish: null }),
	});
});

describe('ActressCandidatesSection', () => {
	it('promotes an ID-only candidate with user-entered canonical fields', async () => {
		const view = renderSection([candidate(1)]);
		promoteCandidate.mockResolvedValue(
			candidate(1, { first_name: 'Yui', japanese_name: '結衣', verified: true }),
		);
		await openCandidates(view);
		const fields = within(view.getByRole('group', { name: /candidate #1/i }));
		await fireEvent.input(fields.getByLabelText('First Name'), { target: { value: 'Yui' } });
		await fireEvent.input(fields.getByLabelText('Japanese Name'), { target: { value: '結衣' } });
		await fireEvent.input(fields.getByLabelText('Last Name'), { target: { value: 'Hatano' } });
		await fireEvent.input(fields.getByLabelText('Thumbnail URL'), {
			target: { value: 'https://example.test/yui.jpg' },
		});
		listCandidates.mockResolvedValue({ candidates: [], total: 0 });
		await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
		await waitFor(() =>
			expect(promoteCandidate).toHaveBeenCalledWith(1, {
				first_name: 'Yui',
				last_name: 'Hatano',
				japanese_name: '結衣',
				thumb_url: 'https://example.test/yui.jpg',
			}),
		);
		await waitFor(() => expect(view.queryByRole('group', { name: /candidate #1/i })).toBeNull());
	});

	it('blocks whitespace-only canonical names with an accessible error', async () => {
		const view = renderSection([candidate(1)]);
		await openCandidates(view);
		const fields = within(view.getByRole('group', { name: /candidate #1/i }));
		const first = fields.getByLabelText('First Name');
		const japanese = fields.getByLabelText('Japanese Name');
		await fireEvent.input(first, { target: { value: '   ' } });
		await fireEvent.input(japanese, { target: { value: '\t' } });
		await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
		const error = fields.getByRole('alert');
		expect(error.textContent).toContain('Either First Name or Japanese Name is required');
		expect(first.getAttribute('aria-invalid')).toBe('true');
		expect(first.getAttribute('aria-describedby')).toBe(error.id);
		expect(japanese.getAttribute('aria-describedby')).toBe(error.id);
		expect(promoteCandidate).not.toHaveBeenCalled();
	});

	it('keeps valid candidates as one-click promotions with their default payload', async () => {
		const existing = candidate(1, {
			first_name: 'Yui',
			last_name: 'Hatano',
			japanese_name: '波多野結衣',
			thumb_url: 'https://example.test/thumb.jpg',
		});
		const view = renderSection([existing]);
		promoteCandidate.mockResolvedValue({ ...existing, verified: true });
		await openCandidates(view);
		await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
		await waitFor(() =>
			expect(promoteCandidate).toHaveBeenCalledWith(1, {
				first_name: 'Yui',
				last_name: 'Hatano',
				japanese_name: '波多野結衣',
				thumb_url: 'https://example.test/thumb.jpg',
			}),
		);
	});

	it('preserves a draft and displays the server error after promotion fails', async () => {
		const view = renderSection([candidate(1)]);
		promoteCandidate.mockRejectedValue(new Error('canonical name conflicts'));
		await openCandidates(view);
		const first = within(view.getByRole('group', { name: /candidate #1/i })).getByLabelText(
			'First Name',
		) as HTMLInputElement;
		await fireEvent.input(first, { target: { value: 'Edited' } });
		await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
		const error = await view.findByText('canonical name conflicts');
		expect(error.getAttribute('role')).toBe('alert');
		expect(first.value).toBe('Edited');
	});

	it('isolates drafts per candidate and retains them across list refreshes', async () => {
		const initial = [candidate(1, { first_name: 'One' }), candidate(2, { japanese_name: '二' })];
		const view = renderSection(initial);
		await openCandidates(view);
		const firstGroup = view.getByRole('group', { name: /candidate #1/i });
		const secondGroup = view.getByRole('group', { name: /candidate #2/i });
		await fireEvent.input(within(firstGroup).getByLabelText('First Name'), {
			target: { value: 'Edited One' },
		});
		listCandidates.mockResolvedValue({
			candidates: [
				candidate(1, { first_name: 'Server One' }),
				candidate(2, { japanese_name: 'Server Two' }),
			],
			total: 2,
		});
		await view.client.invalidateQueries({ queryKey: ['actress-candidates'] });
		await waitFor(() => expect(listCandidates.mock.calls.length).toBeGreaterThan(1));
		const refreshedFirstGroup = await view.findByRole('group', { name: /candidate #1/i });
		const refreshedSecondGroup = view.getByRole('group', { name: /candidate #2/i });
		expect(
			(within(refreshedFirstGroup).getByLabelText('First Name') as HTMLInputElement).value,
		).toBe('Edited One');
		expect(
			(within(refreshedSecondGroup).getByLabelText('Japanese Name') as HTMLInputElement).value,
		).toBe('Server Two');
		await fireEvent.input(within(refreshedSecondGroup).getByLabelText('Japanese Name'), {
			target: { value: 'Edited Two' },
		});
		expect(
			(within(refreshedFirstGroup).getByLabelText('First Name') as HTMLInputElement).value,
		).toBe('Edited One');
	});

	it('does not render or submit candidates without a valid stable ID', async () => {
		const view = renderSection([
			candidate(0),
			{ verified: false },
			candidate(7, { first_name: 'Valid' }),
		]);
		await fireEvent.click(view.getByRole('button', { name: /Candidate identities/i }));
		await view.findByRole('group', { name: /candidate #7/i });
		expect(view.getAllByRole('group')).toHaveLength(1);
		expect(view.queryByRole('group', { name: /candidate #0/i })).toBeNull();
		expect(promoteCandidate).not.toHaveBeenCalled();
	});

	it('rejects malformed and non-http thumbnail URLs with a field-scoped accessible error', async () => {
		const view = renderSection([candidate(1, { first_name: 'Valid' })]);
		await openCandidates(view);
		const fields = within(view.getByRole('group', { name: /candidate #1/i }));
		const thumb = fields.getByLabelText('Thumbnail URL');
		for (const value of ['not a url', 'ftp://example.test/image.jpg']) {
			await fireEvent.input(thumb, { target: { value } });
			await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
			const error = fields.getByRole('alert');
			expect(error.textContent).toContain('valid HTTP or HTTPS URL');
			expect(thumb.getAttribute('aria-invalid')).toBe('true');
			expect(thumb.getAttribute('aria-describedby')).toBe(error.id);
		}
		expect(promoteCandidate).not.toHaveBeenCalled();
	});

	it('locks editing, collapse, and pagination while promoting and updates only the originating cache', async () => {
		let resolvePromotion!: (value: Actress) => void;
		promoteCandidate.mockReturnValue(new Promise((resolve) => (resolvePromotion = resolve)));
		const view = renderSection([candidate(1, { first_name: 'One' })], 101);
		await openCandidates(view);
		const group = view.getByRole('group', { name: /candidate #1/i });
		const first = within(group).getByLabelText('First Name') as HTMLInputElement;
		const toggle = view.getByRole('button', { name: /Candidate identities/i });
		const next = view.getByRole('button', { name: 'Next' });
		view.client.setQueryData(['actress-candidates', 100], {
			candidates: [candidate(2, { first_name: 'Two' })],
			total: 101,
		});

		await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
		await waitFor(() => {
			expect(promoteCandidate).toHaveBeenCalled();
			expect(first.matches(':disabled')).toBe(true);
			expect(toggle.hasAttribute('disabled')).toBe(true);
			expect(next.hasAttribute('disabled')).toBe(true);
			expect(group.getAttribute('aria-busy')).toBe('true');
			expect(within(group).getByRole('status').textContent).toContain('Promoting');
		});

		listCandidates.mockResolvedValue({ candidates: [], total: 99 });
		resolvePromotion(candidate(1, { first_name: 'One', verified: true }));
		await waitFor(() => {
			const origin = view.client.getQueryData<{ candidates: Actress[]; total: number }>([
				'actress-candidates',
				0,
			]);
			expect(origin?.candidates).toHaveLength(0);
			expect(origin?.total).toBe(99);
		});
		const other = view.client.getQueryData<{ candidates: Actress[]; total: number }>([
			'actress-candidates',
			100,
		]);
		expect(other?.candidates.map((item) => item.id)).toEqual([2]);
		expect(other?.total).toBe(101);
	});

	it('scopes server errors to one candidate and clears them on edit and collapse', async () => {
		const view = renderSection([
			candidate(1, { first_name: 'One' }),
			candidate(2, { first_name: 'Two' }),
		]);
		promoteCandidate.mockRejectedValueOnce(new Error('candidate one conflict'));
		await openCandidates(view);
		const firstGroup = view.getByRole('group', { name: /candidate #1/i });
		const secondGroup = view.getByRole('group', { name: /candidate #2/i });
		const firstRow = firstGroup.closest('li') as HTMLLIElement;
		await fireEvent.click(within(firstRow).getByRole('button', { name: /Promote/i }));
		expect((await within(firstGroup).findByRole('alert')).textContent).toContain(
			'candidate one conflict',
		);
		expect(within(secondGroup).queryByRole('alert')).toBeNull();

		await fireEvent.input(within(firstGroup).getByLabelText('Last Name'), {
			target: { value: 'Edited' },
		});
		expect(within(firstGroup).queryByText('candidate one conflict')).toBeNull();

		promoteCandidate.mockRejectedValueOnce(new Error('retry conflict'));
		await fireEvent.click(within(firstRow).getByRole('button', { name: /Promote/i }));
		await within(firstGroup).findByText('retry conflict');
		await fireEvent.click(view.getByRole('button', { name: /Candidate identities/i }));
		await fireEvent.click(view.getByRole('button', { name: /Candidate identities/i }));
		await view.findByRole('group', { name: /candidate #1/i });
		expect(view.queryByText('retry conflict')).toBeNull();
	});

	it('sends explicit empty canonical values when the user clears optional fields', async () => {
		const existing = candidate(1, {
			first_name: 'Yui',
			last_name: 'Wrong',
			japanese_name: '誤名',
			thumb_url: 'https://example.test/wrong.jpg',
		});
		const view = renderSection([existing]);
		promoteCandidate.mockResolvedValue(candidate(1, { first_name: 'Yui', verified: true }));
		await openCandidates(view);
		const fields = within(view.getByRole('group', { name: /candidate #1/i }));
		await fireEvent.input(fields.getByLabelText('Last Name'), { target: { value: '' } });
		await fireEvent.input(fields.getByLabelText('Japanese Name'), { target: { value: '' } });
		await fireEvent.input(fields.getByLabelText('Thumbnail URL'), { target: { value: '' } });
		listCandidates.mockResolvedValue({ candidates: [], total: 0 });
		await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
		await waitFor(() =>
			expect(promoteCandidate).toHaveBeenCalledWith(1, {
				first_name: 'Yui',
				last_name: '',
				japanese_name: '',
				thumb_url: '',
			}),
		);
	});

	it('reconciles untouched fields after leaving and revisiting a page', async () => {
		const view = renderSection(
			[candidate(1, { first_name: 'One', last_name: 'Original Last' })],
			101,
		);
		await openCandidates(view);
		const firstPage = view.getByRole('group', { name: /candidate #1/i });
		await fireEvent.input(within(firstPage).getByLabelText('First Name'), {
			target: { value: 'Edited One' },
		});

		listCandidates.mockResolvedValue({
			candidates: [candidate(2, { first_name: 'Two' })],
			total: 101,
		});
		await fireEvent.click(view.getByRole('button', { name: 'Next' }));
		await view.findByRole('group', { name: /candidate #2/i });

		listCandidates.mockResolvedValue({
			candidates: [candidate(1, { first_name: 'Server One', last_name: 'Server Last' })],
			total: 101,
		});
		await fireEvent.click(view.getByRole('button', { name: 'Prev' }));
		const revisited = await view.findByRole('group', { name: /candidate #1/i });
		await waitFor(() => {
			expect((within(revisited).getByLabelText('First Name') as HTMLInputElement).value).toBe(
				'Edited One',
			);
			expect((within(revisited).getByLabelText('Last Name') as HTMLInputElement).value).toBe(
				'Server Last',
			);
		});
	});

	it('clears a candidate-scoped server error when changing pages', async () => {
		const view = renderSection([candidate(1, { first_name: 'One' })], 101);
		promoteCandidate.mockRejectedValueOnce(new Error('page-scoped conflict'));
		await openCandidates(view);
		const firstRow = view
			.getByRole('group', { name: /candidate #1/i })
			.closest('li') as HTMLLIElement;
		await fireEvent.click(within(firstRow).getByRole('button', { name: /Promote/i }));
		await within(firstRow).findByText('page-scoped conflict');

		listCandidates.mockResolvedValue({
			candidates: [candidate(2, { first_name: 'Two' })],
			total: 101,
		});
		await fireEvent.click(view.getByRole('button', { name: 'Next' }));
		await view.findByRole('group', { name: /candidate #2/i });

		listCandidates.mockResolvedValue({
			candidates: [candidate(1, { first_name: 'One' })],
			total: 101,
		});
		await fireEvent.click(view.getByRole('button', { name: 'Prev' }));
		const revisited = await view.findByRole('group', { name: /candidate #1/i });
		expect(within(revisited).queryByText('page-scoped conflict')).toBeNull();
	});

	it('preserves a cleared field across a background refetch while adopting untouched fields', async () => {
		const view = renderSection([candidate(1, { first_name: 'Original', last_name: 'Wrong' })]);
		await openCandidates(view);
		const fields = within(view.getByRole('group', { name: /candidate #1/i }));
		await fireEvent.input(fields.getByLabelText('Last Name'), { target: { value: '' } });

		listCandidates.mockResolvedValue({
			candidates: [candidate(1, { first_name: 'Server First', last_name: 'Wrong' })],
			total: 1,
		});
		await view.client.invalidateQueries({ queryKey: ['actress-candidates'] });
		const refreshed = within(await view.findByRole('group', { name: /candidate #1/i }));
		await waitFor(() => {
			expect((refreshed.getByLabelText('First Name') as HTMLInputElement).value).toBe(
				'Server First',
			);
			expect((refreshed.getByLabelText('Last Name') as HTMLInputElement).value).toBe('');
		});

		promoteCandidate.mockResolvedValue(
			candidate(1, { first_name: 'Server First', verified: true }),
		);
		listCandidates.mockResolvedValue({ candidates: [], total: 0 });
		await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
		await waitFor(() =>
			expect(promoteCandidate).toHaveBeenCalledWith(1, {
				first_name: 'Server First',
				last_name: '',
				japanese_name: '',
				thumb_url: '',
			}),
		);
	});

	it('keeps drafted rows mounted during a background refetch', async () => {
		const view = renderSection([candidate(1, { first_name: 'One' })]);
		await openCandidates(view);
		const fields = within(view.getByRole('group', { name: /candidate #1/i }));
		await fireEvent.input(fields.getByLabelText('First Name'), { target: { value: 'Edited' } });

		let resolveList!: (value: { candidates: Actress[]; total: number }) => void;
		listCandidates.mockReturnValue(new Promise((resolve) => (resolveList = resolve)));
		const callsBefore = listCandidates.mock.calls.length;
		const refetch = view.client.invalidateQueries({ queryKey: ['actress-candidates'] });
		await waitFor(() => expect(listCandidates.mock.calls.length).toBeGreaterThan(callsBefore));

		const stillMounted = view.getByRole('group', { name: /candidate #1/i });
		expect((within(stillMounted).getByLabelText('First Name') as HTMLInputElement).value).toBe(
			'Edited',
		);

		resolveList({ candidates: [candidate(1, { first_name: 'One' })], total: 1 });
		await refetch;
	});

	it('notifies the parent with the promoted actress after success', async () => {
		listCandidates.mockResolvedValue({
			candidates: [candidate(1, { first_name: 'One' })],
			total: 1,
		});
		const onPromoted = vi.fn();
		const client = new QueryClient({
			defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
		});
		const view = render(
			ActressCandidatesSection,
			{ onPromoted },
			{ wrapper: QueryClientWrapper, wrapperProps: { client } },
		);
		promoteCandidate.mockResolvedValue(candidate(1, { first_name: 'One', verified: true }));
		await fireEvent.click(view.getByRole('button', { name: /Candidate identities/i }));
		await view.findByRole('group', { name: /candidate #1/i });
		listCandidates.mockResolvedValue({ candidates: [], total: 0 });
		await fireEvent.click(view.getByRole('button', { name: /Promote/i }));
		await waitFor(() =>
			expect(onPromoted).toHaveBeenCalledWith(expect.objectContaining({ id: 1, verified: true })),
		);
	});

	it('gives each promote button a candidate-specific accessible name', async () => {
		const view = renderSection([
			candidate(1, { japanese_name: '結衣' }),
			candidate(2, { first_name: 'Yui', last_name: 'Hatano' }),
			candidate(3),
		]);
		await openCandidates(view);
		expect(view.getByRole('button', { name: 'Promote 結衣' })).toBeTruthy();
		expect(view.getByRole('button', { name: 'Promote Hatano Yui' })).toBeTruthy();
		expect(view.getByRole('button', { name: 'Promote #3' })).toBeTruthy();
		const fields = within(view.getByRole('group', { name: /candidate #3/i }));
		await fireEvent.input(fields.getByLabelText('Japanese Name'), { target: { value: 'みなみ' } });
		expect(view.getByRole('button', { name: 'Promote みなみ' })).toBeTruthy();
	});
});
