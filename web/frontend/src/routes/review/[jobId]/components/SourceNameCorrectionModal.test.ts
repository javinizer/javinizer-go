import { cleanup, fireEvent, render, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from '$lib/api/client';
import { ApiError } from '$lib/api/clients/common';
import type { ActressNameOverride, CastNameProvenance } from '$lib/api/types';
import SourceNameCorrectionModal from './SourceNameCorrectionModal.svelte';

const { invalidateQueries } = vi.hoisted(() => ({
	invalidateQueries: vi.fn().mockResolvedValue(undefined),
}));
vi.mock('$lib/query/client', () => ({ getQueryClient: () => ({ invalidateQueries }) }));

const provenance: CastNameProvenance = {
	source: 'javlibrary',
	source_id: 'ab123',
	display_name: 'Lee Mary Jane',
	state: 'unknown_order',
};
const rule: ActressNameOverride = {
	source_name: provenance.display_name,
	first_name: 'Mary Jane',
	last_name: 'Lee',
};
function deferred<T>() {
	let resolve!: (value: T) => void;
	const promise = new Promise<T>((r) => {
		resolve = r;
	});
	return { promise, resolve };
}
async function mount(existing: ActressNameOverride | null = null, source = provenance) {
	const get = vi.spyOn(apiClient.scrapers, 'getNameOverride').mockResolvedValue({ rule: existing });
	const save = vi.spyOn(apiClient.scrapers, 'saveNameOverride').mockResolvedValue({ rule });
	const onClose = vi.fn();
	const view = render(SourceNameCorrectionModal, { provenance: source, onClose });
	await waitFor(() =>
		expect((view.getByLabelText('Given name') as HTMLInputElement).disabled).toBe(false),
	);
	return { view, get, save, onClose };
}
async function fill(
	view: Awaited<ReturnType<typeof mount>>['view'],
	first = 'Mary Jane',
	last = 'Lee',
) {
	await fireEvent.input(view.getByLabelText('Given name'), { target: { value: first } });
	await fireEvent.input(view.getByLabelText('Family name'), { target: { value: last } });
	await fireEvent.click(
		view.getByRole('checkbox', { name: 'Use this correction for future scrapes only' }),
	);
}
afterEach(() => {
	cleanup();
	invalidateQueries.mockClear();
	vi.restoreAllMocks();
});

describe('source name correction dialog', () => {
	it('starts unknown names blank and requires reconstruction plus scope confirmation', async () => {
		const { view, save } = await mount();
		expect((view.getByLabelText('Given name') as HTMLInputElement).value).toBe('');
		expect((view.getByLabelText('Family name') as HTMLInputElement).value).toBe('');
		const submit = view.getByRole('button', {
			name: 'Save for future scrapes',
		}) as HTMLButtonElement;
		expect(submit.disabled).toBe(true);
		await fill(view, 'Mary', 'Lee');
		expect(submit.disabled).toBe(true);
		await fireEvent.input(view.getByLabelText('Given name'), { target: { value: 'Mary Jane' } });
		expect(submit.disabled).toBe(true);
		await fireEvent.click(view.getByRole('checkbox'));
		expect(submit.disabled).toBe(false);
		await fireEvent.click(submit);
		await view.findByText('Saved for future scrapes.');
		expect(save).toHaveBeenCalledExactlyOnceWith('ab123', { ...rule, expected_rule: null });
		expect(view.getByText(/current movie and historical credits are unchanged/)).toBeTruthy();
		expect(invalidateQueries).toHaveBeenCalledExactlyOnceWith({ queryKey: ['config'] });
		const outside = document.createElement('button');
		document.body.append(outside);
		outside.focus();
		await fireEvent.keyDown(window, { key: 'Tab' });
		expect(document.activeElement).toBe(view.getByRole('button', { name: 'Done' }));
		outside.remove();
	});

	it('swaps multiword parts without guessing and requires renewed acknowledgment', async () => {
		const { view, save } = await mount(rule);
		await fireEvent.click(view.getByRole('checkbox'));
		await fireEvent.click(view.getByRole('button', { name: 'Swap names' }));
		expect((view.getByLabelText('Given name') as HTMLInputElement).value).toBe('Lee');
		expect((view.getByLabelText('Family name') as HTMLInputElement).value).toBe('Mary Jane');
		expect((view.getByRole('checkbox') as HTMLInputElement).checked).toBe(false);
		await fireEvent.click(view.getByRole('checkbox'));
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByText('Saved for future scrapes.');
		expect(save).toHaveBeenCalledWith('ab123', {
			...rule,
			first_name: 'Lee',
			last_name: 'Mary Jane',
			expected_rule: rule,
		});
	});

	it('cancels or escapes without a write', async () => {
		const { view, save, onClose } = await mount();
		await fill(view);
		await fireEvent.click(view.getByRole('button', { name: 'Cancel' }));
		expect(onClose).toHaveBeenCalledOnce();
		await fireEvent.keyDown(window, { key: 'Escape' });
		expect(onClose).toHaveBeenCalledTimes(2);
		expect(save).not.toHaveBeenCalled();
	});

	it('blocks duplicate submit and dismissal while saving', async () => {
		const { view, save, onClose } = await mount();
		const pending = deferred<{ rule: ActressNameOverride }>();
		save.mockReturnValue(pending.promise);
		await fill(view);
		const form = view.getByLabelText('Given name').closest('form')!;
		await Promise.all([fireEvent.submit(form), fireEvent.submit(form)]);
		expect(save).toHaveBeenCalledOnce();
		expect((view.getByRole('button', { name: 'Cancel' }) as HTMLButtonElement).disabled).toBe(true);
		await fireEvent.keyDown(window, { key: 'Escape' });
		expect(onClose).not.toHaveBeenCalled();
		pending.resolve({ rule });
		await view.findByText('Saved for future scrapes.');
	});

	it('requires explicit reload and new confirmation after a stale-rule conflict', async () => {
		const { view, save, get } = await mount();
		save.mockRejectedValueOnce(new ApiError('stale', undefined, undefined, 409));
		await fill(view);
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByRole('alert');
		expect(view.getByRole('alert').textContent).toContain('changed while you were editing');
		expect(save).toHaveBeenCalledOnce();
		expect((view.getByLabelText('Given name') as HTMLInputElement).value).toBe('Mary Jane');
		expect(
			(view.getByRole('button', { name: 'Save for future scrapes' }) as HTMLButtonElement).disabled,
		).toBe(true);
		get.mockResolvedValueOnce({ rule });
		await fireEvent.click(view.getByRole('button', { name: 'Reload saved correction' }));
		await waitFor(() =>
			expect((view.getByLabelText('Given name') as HTMLInputElement).disabled).toBe(false),
		);
		expect((view.getByRole('checkbox') as HTMLInputElement).checked).toBe(false);
		await fireEvent.click(view.getByRole('checkbox'));
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByText('Saved for future scrapes.');
		expect(save).toHaveBeenLastCalledWith('ab123', { ...rule, expected_rule: rule });
	});

	it('preserves the draft after an uncertain save and checks the server before retry', async () => {
		const { view, save, get } = await mount();
		save.mockRejectedValueOnce(new TypeError('network unavailable'));
		await fill(view);
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByRole('alert');
		expect(view.getByRole('alert').textContent).toContain('Could not confirm the save');
		expect(invalidateQueries).not.toHaveBeenCalled();
		expect((view.getByLabelText('Given name') as HTMLInputElement).value).toBe('Mary Jane');
		expect(save).toHaveBeenCalledOnce();
		expect(get).toHaveBeenCalledOnce();
		get.mockResolvedValueOnce({ rule });
		await fireEvent.click(view.getByRole('button', { name: 'Reload saved correction' }));
		await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
		await waitFor(() =>
			expect((view.getByLabelText('Given name') as HTMLInputElement).disabled).toBe(false),
		);
		expect((view.getByRole('checkbox') as HTMLInputElement).checked).toBe(false);
		expect(save).toHaveBeenCalledOnce();
	});

	it('requires configuration recovery after rollback failure rather than offering an unsafe reload retry', async () => {
		const { view, save, get, onClose } = await mount();
		save.mockRejectedValueOnce(
			new ApiError('rollback failed', 'NAME_OVERRIDE_ROLLBACK_FAILED', undefined, 500),
		);
		await fill(view);
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByRole('alert');
		expect(view.getByRole('alert').textContent).toContain('Configuration could not be restored');
		expect(view.queryByRole('button', { name: 'Reload saved correction' })).toBeNull();
		expect(
			(view.getByRole('button', { name: 'Save for future scrapes' }) as HTMLButtonElement).disabled,
		).toBe(true);
		expect(get).toHaveBeenCalledOnce();
		expect(save).toHaveBeenCalledOnce();
		expect(invalidateQueries).not.toHaveBeenCalled();
		await fireEvent.click(view.getByRole('button', { name: 'Cancel' }));
		expect(onClose).toHaveBeenCalledOnce();
	});

	it('does not prefill a saved rule for a different source label but preserves its CAS guard', async () => {
		const old = { source_name: 'Former Name', first_name: 'Name', last_name: 'Former' };
		const { view, save } = await mount(old);
		expect(view.getByText(/saved correction is for a different source label/)).toBeTruthy();
		expect((view.getByLabelText('Given name') as HTMLInputElement).value).toBe('');
		await fill(view);
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByText('Saved for future scrapes.');
		expect(save).toHaveBeenCalledWith('ab123', { ...rule, expected_rule: old });
	});

	it('treats whitespace-only label changes as the same guard while preserving exact CAS evidence', async () => {
		const prior = { ...rule, source_name: ' Lee\u0085Mary  Jane ' };
		const { view, save } = await mount(prior);
		expect(view.queryByText(/saved correction is for a different source label/)).toBeNull();
		expect((view.getByLabelText('Given name') as HTMLInputElement).value).toBe('Mary Jane');
		await fireEvent.click(view.getByRole('checkbox'));
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByText('Saved for future scrapes.');
		expect(save).toHaveBeenCalledWith('ab123', { ...rule, expected_rule: prior });
	});

	it('preserves entity-like spelling literally through display and submission', async () => {
		const { view, save } = await mount(null, { ...provenance, display_name: 'Lee &amp; Jane' });
		expect(view.getByText('Lee &amp; Jane')).toBeTruthy();
		expect(view.queryByText('Lee & Jane')).toBeNull();
		await fill(view, '&amp; Jane', 'Lee');
		await fireEvent.click(view.getByRole('button', { name: 'Save for future scrapes' }));
		await view.findByText('Saved for future scrapes.');
		expect(save).toHaveBeenCalledWith('ab123', {
			source_name: 'Lee &amp; Jane',
			first_name: '&amp; Jane',
			last_name: 'Lee',
			expected_rule: null,
		});
	});

	it('keeps save blocked after a read failure and allows a deliberate reload', async () => {
		const get = vi
			.spyOn(apiClient.scrapers, 'getNameOverride')
			.mockRejectedValueOnce(new Error('offline'))
			.mockResolvedValue({ rule: null });
		const save = vi.spyOn(apiClient.scrapers, 'saveNameOverride');
		const view = render(SourceNameCorrectionModal, { provenance, onClose: vi.fn() });
		await view.findByRole('alert');
		expect(
			(view.getByRole('button', { name: 'Save for future scrapes' }) as HTMLButtonElement).disabled,
		).toBe(true);
		await fireEvent.click(view.getByRole('button', { name: 'Reload saved correction' }));
		await waitFor(() =>
			expect((view.getByLabelText('Given name') as HTMLInputElement).disabled).toBe(false),
		);
		expect(get).toHaveBeenCalledTimes(2);
		expect(save).not.toHaveBeenCalled();
	});

	it('blocks a GET configuration-unavailable response without allowing a stale-snapshot retry', async () => {
		const get = vi
			.spyOn(apiClient.scrapers, 'getNameOverride')
			.mockRejectedValue(
				new ApiError('unavailable', 'NAME_OVERRIDE_CONFIG_UNAVAILABLE', undefined, 503),
			);
		const save = vi.spyOn(apiClient.scrapers, 'saveNameOverride');
		const onClose = vi.fn();
		const view = render(SourceNameCorrectionModal, { provenance, onClose });
		await view.findByRole('alert');
		expect(view.getByRole('alert').textContent).toContain(
			'saved configuration is unavailable or out of sync',
		);
		expect(view.queryByRole('button', { name: 'Reload saved correction' })).toBeNull();
		expect(
			(view.getByRole('button', { name: 'Save for future scrapes' }) as HTMLButtonElement).disabled,
		).toBe(true);
		expect(get).toHaveBeenCalledOnce();
		expect(save).not.toHaveBeenCalled();
		await fireEvent.click(view.getByRole('button', { name: 'Cancel' }));
		expect(onClose).toHaveBeenCalledOnce();
	});

	it('ignores a late read after unmounting', async () => {
		const pending = deferred<{ rule: ActressNameOverride | null }>();
		const get = vi.spyOn(apiClient.scrapers, 'getNameOverride').mockReturnValue(pending.promise);
		const view = render(SourceNameCorrectionModal, { provenance, onClose: vi.fn() });
		await waitFor(() => expect(get).toHaveBeenCalledOnce());
		const signal = get.mock.calls[0][1]?.signal;
		view.unmount();
		expect(signal?.aborted).toBe(true);
		pending.resolve({ rule });
		await Promise.resolve();
		expect(document.querySelector('[aria-labelledby="name-correction-title"]')).toBeNull();
	});
});
