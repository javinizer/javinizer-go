import { cleanup, fireEvent, render, within } from '@testing-library/svelte';
import { afterEach, expect, it, vi } from 'vitest';
import type {
	ActressMergeField,
	ActressMergePolicy,
	ActressMergePreviewResponse,
} from '$lib/api/types';
import ActressMergeModal from './ActressMergeModal.svelte';

afterEach(cleanup);

function preview(policy: ActressMergePolicy = 'fill_empty'): ActressMergePreviewResponse {
	const fields: ActressMergeField[] = [
		{
			field: 'dmm_id',
			target_value: 0,
			source_value: 123,
			default_resolution: 'source',
			result_value: 123,
		},
		{
			field: 'first_name',
			target_value: 'Target',
			source_value: 'Source',
			default_resolution: 'target',
			result_value: 'Target',
		},
		{
			field: 'last_name',
			target_value: '',
			source_value: 'Last',
			default_resolution: 'source',
			result_value: 'Last',
		},
		{
			field: 'japanese_name',
			target_value: '',
			source_value: '',
			default_resolution: 'target',
			result_value: '',
		},
		{
			field: 'thumb_url',
			target_value: '',
			source_value: '',
			default_resolution: 'target',
			result_value: '',
		},
	];
	if (policy === 'preserve_target') {
		fields.forEach((field) => {
			field.default_resolution = 'target';
			field.result_value = field.target_value ?? '';
		});
	}
	return {
		policy,
		target: { id: 1, dmm_id: 0 },
		source: { id: 2, dmm_id: 123 },
		proposed_merged: { id: 1 },
		fields,
		conflicts: [],
		default_resolutions: {},
	};
}

function props(policy: ActressMergePolicy = 'fill_empty') {
	return {
		showMergeModal: true,
		selectedIds: [1, 2],
		mergePrimaryId: 1,
		mergeSourceQueue: [2],
		mergeCurrentSourceId: 2,
		mergeResolutions: {},
		mergePolicy: policy,
		mergePreview: preview(policy),
		mergePreviewFetching: false,
		mergeSummary: { success: 0, failed: 0, messages: [] },
		mergePending: false,
		getActressLabelByID: (id: number) => `Actress #${id}`,
		onCloseMergeModal: vi.fn(),
		onResetMergeQueueAndPreview: vi.fn(),
		onApplyCurrentMerge: vi.fn(),
		onSkipCurrentMerge: vi.fn(),
		onSetResolution: vi.fn(),
		onSetMergePolicy: vi.fn(),
		formatMergeValue: (value: unknown) =>
			value === '' || value == null ? '(empty)' : String(value),
	};
}

it('shows all five resulting fields including blanks, even without conflicts', () => {
	const input = props();
	const view = render(ActressMergeModal, input);
	expect(view.getAllByRole('group')).toHaveLength(5);
	for (const label of ['DMM ID', 'First Name', 'Last Name', 'Japanese Name', 'Thumbnail URL']) {
		const field = within(view.getByRole('group', { name: label }));
		expect(field.getAllByRole('radio')).toHaveLength(3);
		expect((field.getByRole('radio', { name: /Use policy/ }) as HTMLInputElement).checked).toBe(
			true,
		);
	}
	expect(view.getByText('Result: 123')).toBeTruthy();
	expect(view.getByText('Result: Last')).toBeTruthy();
	expect(view.getAllByText('Result: (empty)')).toHaveLength(2);
	expect(view.getByText(/Source actress #2 will be deleted/)).toBeTruthy();
	expect(view.queryByRole('alert')).toBeNull();
	expect(view.queryByText(/Safe to merge/)).toBeNull();
	expect(input.onSetResolution).not.toHaveBeenCalled();
});

it('offers both policies and explains that per-field choices override them', async () => {
	const input = props();
	const view = render(ActressMergeModal, input);
	const select = view.getByRole('combobox', { name: 'Merge field policy' });
	expect((select as HTMLSelectElement).value).toBe('fill_empty');
	expect(view.getByRole('option', { name: 'Preserve target, including blanks' })).toBeTruthy();
	await fireEvent.change(select, { target: { value: 'preserve_target' } });
	expect(input.onSetMergePolicy).toHaveBeenCalledWith('preserve_target');
	expect(
		view.getByText(/The policy applies unless you choose a target or source value/),
	).toBeTruthy();
});

it('warns when keeping DMM zero discards a positive source ID and permits choosing source', async () => {
	const input = props('preserve_target');
	const view = render(ActressMergeModal, input);
	expect(view.getByRole('alert').textContent).toContain(
		'Keeping DMM ID 0 discards the source DMM ID 123',
	);
	expect(view.getByText('Result: 0')).toBeTruthy();
	const dmm = within(view.getByRole('group', { name: 'DMM ID' }));
	await fireEvent.click(dmm.getByRole('radio', { name: /Use source/ }));
	expect(input.onSetResolution).toHaveBeenCalledWith('dmm_id', 'source');
	const updated = preview('preserve_target');
	updated.fields[0].result_value = 123;
	await view.rerender({ mergePreview: updated, mergeResolutions: { dmm_id: 'source' } });
	expect(view.queryByRole('alert')).toBeNull();
	expect(view.getByText('Result: 123')).toBeTruthy();
});

it('supports explicit target blanks and returning a field to policy control', async () => {
	const input = props();
	const view = render(ActressMergeModal, input);
	const field = within(view.getByRole('group', { name: 'Last Name' }));
	await fireEvent.click(field.getByRole('radio', { name: /Keep target/ }));
	expect(input.onSetResolution).toHaveBeenCalledWith('last_name', 'target');
	await view.rerender({ mergeResolutions: { last_name: 'target' } });
	await fireEvent.click(field.getByRole('radio', { name: /Use policy/ }));
	expect(input.onSetResolution).toHaveBeenLastCalledWith('last_name', undefined);
});

it('removes stale result controls while fetching a changed preview', () => {
	const view = render(ActressMergeModal, { ...props(), mergePreviewFetching: true });
	expect(view.getByText('Loading merge preview...')).toBeTruthy();
	expect(view.queryByRole('button', { name: 'Apply Merge' })).toBeNull();
	expect(view.queryAllByRole('group')).toHaveLength(0);
});

it('disables close, pair selection, policy, field controls, skip and apply while submitting', () => {
	const view = render(ActressMergeModal, { ...props(), mergePending: true });
	for (const element of [
		...view.getAllByRole('combobox'),
		...view.getAllByRole('radio'),
		...view.getAllByRole('button'),
	]) {
		expect(element.matches(':disabled')).toBe(true);
	}
});
