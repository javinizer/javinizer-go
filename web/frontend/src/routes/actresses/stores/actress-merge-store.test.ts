import { cleanup, render, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import { apiClient } from '$lib/api/client';
import type {
	Actress,
	ActressMergeField,
	ActressMergePreviewRequest,
	ActressMergePreviewResponse,
	ActressMergeResponse,
} from '$lib/api/types';
import { createActressStore } from './actress-store.svelte';
import Harness from '../../../../test/harness/ActressStoreHarness.svelte';

const clients: QueryClient[] = [];
const records: Actress[] = [
	{ id: 1, first_name: 'Target', dmm_id: 0, last_name: '', japanese_name: '', thumb_url: '' },
	{
		id: 2,
		first_name: 'Source',
		dmm_id: 123,
		last_name: 'Last',
		japanese_name: '名前',
		thumb_url: 'https://example.test/source.jpg',
	},
	{ id: 3, first_name: 'Next', dmm_id: 456 },
];
const fields: ActressMergeField['field'][] = [
	'dmm_id',
	'first_name',
	'last_name',
	'japanese_name',
	'thumb_url',
];

function preview(request: ActressMergePreviewRequest): ActressMergePreviewResponse {
	const target = records.find((record) => record.id === request.target_id)!;
	const source = records.find((record) => record.id === request.source_id)!;
	const policy = request.policy ?? 'fill_empty';
	const proposed = { ...target };
	const results = fields.map((field): ActressMergeField => {
		const targetValue = target[field] ?? (field === 'dmm_id' ? 0 : '');
		const sourceValue = source[field] ?? (field === 'dmm_id' ? 0 : '');
		const defaultResolution = policy === 'fill_empty' && !targetValue ? 'source' : 'target';
		const resolution = request.resolutions?.[field] ?? defaultResolution;
		const result = resolution === 'target' ? targetValue : sourceValue;
		Object.assign(proposed, { [field]: result });
		return {
			field,
			target_value: targetValue,
			source_value: sourceValue,
			default_resolution: defaultResolution,
			result_value: result,
		};
	});
	return {
		target,
		source,
		policy,
		fields: results,
		proposed_merged: proposed,
		conflicts: [results[1]],
		default_resolutions: { first_name: 'target' },
	};
}

beforeEach(() => {
	vi.spyOn(apiClient, 'listActresses').mockResolvedValue({
		actresses: records,
		count: 3,
		total: 3,
		limit: 20,
		offset: 0,
	});
	vi.spyOn(apiClient, 'previewActressMerge').mockImplementation(async (request) =>
		preview(request),
	);
	vi.spyOn(apiClient, 'mergeActresses').mockImplementation(async (request) => ({
		merged_actress: preview(request).proposed_merged,
		merged_from_id: request.source_id,
		updated_movies: 0,
		conflicts_resolved: 0,
		aliases_added: 0,
	}));
});

afterEach(() => {
	cleanup();
	clients.splice(0).forEach((client) => client.clear());
	vi.restoreAllMocks();
});

async function mount(ids = [1, 2]) {
	const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
	clients.push(client);
	let store!: ReturnType<typeof createActressStore>;
	render(
		Harness,
		{
			capture: (value) => {
				store = value;
			},
		},
		{ wrapper: QueryClientWrapper, wrapperProps: { client } },
	);
	await waitFor(() => expect(store.total).toBe(3));
	flushSync(() => {
		ids.forEach((id) => store.toggleSelection(records[id - 1]));
		store.startMergeSelected();
	});
	await ready(store);
	return { store, client };
}

async function ready(store: ReturnType<typeof createActressStore>) {
	await waitFor(() => {
		expect(store.mergePreview).not.toBeNull();
		expect(store.mergePreviewQuery.isFetching).toBe(false);
	});
}

it('defaults to fill-empty without copying conflict defaults into explicit overrides', async () => {
	const { store } = await mount();
	expect(store.mergePolicy).toBe('fill_empty');
	expect(store.mergeResolutions).toEqual({});
	expect(apiClient.previewActressMerge).toHaveBeenLastCalledWith({
		target_id: 1,
		source_id: 2,
		policy: 'fill_empty',
		resolutions: {},
	});
	expect(store.mergePreview?.proposed_merged.dmm_id).toBe(123);
	expect(store.mergePreview?.fields).toHaveLength(5);
});

it('previews preserve-target, explicit blank overrides, clearing overrides, and submits only explicit decisions', async () => {
	const { store } = await mount();
	flushSync(() => store.setMergePolicy('preserve_target'));
	await ready(store);
	expect(store.mergePreview?.proposed_merged.last_name).toBe('');
	expect(store.mergePreview?.proposed_merged.dmm_id).toBe(0);
	flushSync(() => store.setResolution('last_name', 'source'));
	await ready(store);
	expect(store.mergePreview?.proposed_merged.last_name).toBe('Last');
	flushSync(() => store.setMergePolicy('fill_empty'));
	await ready(store);
	expect(store.mergeResolutions).toEqual({ last_name: 'source' });
	flushSync(() => store.setResolution('last_name', 'target'));
	await ready(store);
	expect(store.mergePreview?.proposed_merged.last_name).toBe('');
	flushSync(() => store.setResolution('last_name', undefined));
	await ready(store);
	expect(store.mergeResolutions).toEqual({});
	expect(store.mergePreview?.proposed_merged.last_name).toBe('Last');
	flushSync(() => {
		store.setMergePolicy('preserve_target');
		store.setResolution('dmm_id', 'source');
	});
	await ready(store);
	flushSync(() => store.applyCurrentMerge());
	await waitFor(() =>
		expect(apiClient.mergeActresses).toHaveBeenCalledWith({
			target_id: 1,
			source_id: 2,
			policy: 'preserve_target',
			resolutions: { dmm_id: 'source' },
		}),
	);
});

it('resets policy and overrides on skip, target changes, and close/reopen', async () => {
	const { store } = await mount([1, 2, 3]);
	flushSync(() => {
		store.setMergePolicy('preserve_target');
		store.setResolution('first_name', 'source');
		store.skipCurrentMerge();
	});
	await ready(store);
	expect(store.mergeCurrentSourceId).toBe(3);
	expect(store.mergePolicy).toBe('fill_empty');
	expect(store.mergeResolutions).toEqual({});
	flushSync(() => {
		store.setMergePolicy('preserve_target');
		store.setResolution('first_name', 'source');
		store.mergePrimaryId = 2;
		store.resetMergeQueueAndPreview();
	});
	await ready(store);
	expect(store.mergeCurrentSourceId).toBe(1);
	expect(store.mergePolicy).toBe('fill_empty');
	expect(store.mergeResolutions).toEqual({});
	flushSync(() => {
		store.setMergePolicy('preserve_target');
		store.setResolution('dmm_id', 'target');
		store.closeMergeModal();
	});
	expect(store.mergePreview).toBeNull();
	flushSync(() => store.startMergeSelected());
	await ready(store);
	expect(store.mergePrimaryId).toBe(1);
	expect(store.mergePolicy).toBe('fill_empty');
	expect(store.mergeResolutions).toEqual({});
});

it('resets policy and overrides before advancing after a completed merge', async () => {
	const { store } = await mount([1, 2, 3]);
	flushSync(() => {
		store.setMergePolicy('preserve_target');
		store.setResolution('first_name', 'source');
	});
	await ready(store);
	flushSync(() => store.applyCurrentMerge());
	await waitFor(() => expect(store.mergeCurrentSourceId).toBe(3));
	await ready(store);
	expect(store.mergePolicy).toBe('fill_empty');
	expect(store.mergeResolutions).toEqual({});
	expect(store.mergeSummary.success).toBe(1);
});

it('does not apply or display a late preview for another policy or a closed session', async () => {
	const { store } = await mount();
	let resolveOld!: (value: ActressMergePreviewResponse) => void;
	vi.mocked(apiClient.previewActressMerge).mockImplementation((request) =>
		request.policy === 'preserve_target'
			? new Promise((resolve) => {
					resolveOld = resolve;
				})
			: Promise.resolve(preview(request)),
	);
	flushSync(() => store.setMergePolicy('preserve_target'));
	await waitFor(() => expect(resolveOld).toBeTypeOf('function'));
	flushSync(() => store.applyCurrentMerge());
	expect(apiClient.mergeActresses).not.toHaveBeenCalled();
	flushSync(() => {
		store.closeMergeModal();
		store.startMergeSelected();
	});
	await ready(store);
	resolveOld(preview({ target_id: 1, source_id: 2, policy: 'preserve_target' }));
	await waitFor(() => expect(store.mergePreview?.policy).toBe('fill_empty'));
	expect(store.mergePreview?.proposed_merged.dmm_id).toBe(123);
	expect(store.mergeResolutions).toEqual({});
});

it('rejects submission until an updated override preview has completed', async () => {
	const { store } = await mount();
	let resolvePreview!: (value: ActressMergePreviewResponse) => void;
	vi.mocked(apiClient.previewActressMerge).mockReturnValue(
		new Promise((resolve) => {
			resolvePreview = resolve;
		}),
	);
	flushSync(() => store.setResolution('dmm_id', 'target'));
	flushSync(() => store.applyCurrentMerge());
	expect(apiClient.mergeActresses).not.toHaveBeenCalled();
	await waitFor(() => expect(store.mergePreviewQuery.isFetching).toBe(true));
	resolvePreview(preview({ target_id: 1, source_id: 2, resolutions: { dmm_id: 'target' } }));
	await ready(store);
	flushSync(() => store.applyCurrentMerge());
	await waitFor(() => expect(apiClient.mergeActresses).toHaveBeenCalledTimes(1));
});

it('locks close, pair changes, policy and overrides while a merge is pending', async () => {
	const { store } = await mount([1, 2, 3]);
	let resolveMerge!: (value: ActressMergeResponse) => void;
	vi.mocked(apiClient.mergeActresses).mockReturnValue(
		new Promise((resolve) => {
			resolveMerge = resolve;
		}),
	);
	flushSync(() => store.applyCurrentMerge());
	await waitFor(() => expect(store.mergeActressMutation.isPending).toBe(true));
	flushSync(() => {
		store.closeMergeModal();
		store.showMergeModal = false;
		store.mergePrimaryId = 3;
		store.resetMergeQueueAndPreview();
		store.setMergePolicy('preserve_target');
		store.setResolution('dmm_id', 'target');
		store.skipCurrentMerge();
		store.applyCurrentMerge();
	});
	expect(store.showMergeModal).toBe(true);
	expect(store.mergePrimaryId).toBe(1);
	expect(store.mergeCurrentSourceId).toBe(2);
	expect(store.mergePolicy).toBe('fill_empty');
	expect(store.mergeResolutions).toEqual({});
	expect(apiClient.mergeActresses).toHaveBeenCalledTimes(1);
	resolveMerge({
		merged_actress: records[0],
		merged_from_id: 2,
		updated_movies: 0,
		conflicts_resolved: 0,
		aliases_added: 0,
	});
	await waitFor(() => expect(store.mergeSummary.success).toBe(1));
});

it('preserves explicit decisions across a background refetch', async () => {
	const { store, client } = await mount();
	flushSync(() => store.setResolution('dmm_id', 'target'));
	await ready(store);
	await client.invalidateQueries({ queryKey: ['actress-merge-preview'] });
	await ready(store);
	expect(store.mergeResolutions).toEqual({ dmm_id: 'target' });
	expect(store.mergePreview?.proposed_merged.dmm_id).toBe(0);
});

it('ignores repeated submission and navigation in the same event turn', async () => {
	const { store } = await mount();
	let resolveMerge!: (value: ActressMergeResponse) => void;
	vi.mocked(apiClient.mergeActresses).mockReturnValue(
		new Promise((resolve) => {
			resolveMerge = resolve;
		}),
	);
	flushSync(() => {
		store.applyCurrentMerge();
		store.applyCurrentMerge();
		store.closeMergeModal();
	});
	await waitFor(() => expect(apiClient.mergeActresses).toHaveBeenCalledTimes(1));
	expect(store.showMergeModal).toBe(true);
	resolveMerge({
		merged_actress: records[0],
		merged_from_id: 2,
		updated_movies: 0,
		conflicts_resolved: 0,
		aliases_added: 0,
	});
	await waitFor(() => expect(store.mergeSummary.success).toBe(1));
});

it('does not skip a new pair when an old preview fails after navigation', async () => {
	const { store } = await mount([1, 2, 3]);
	let rejectOld!: (error: Error) => void;
	vi.mocked(apiClient.previewActressMerge).mockImplementation((request) =>
		request.source_id === 2
			? new Promise((_resolve, reject) => {
					rejectOld = reject;
				})
			: Promise.resolve(preview(request)),
	);
	flushSync(() => store.setMergePolicy('preserve_target'));
	await waitFor(() => expect(rejectOld).toBeTypeOf('function'));
	flushSync(() => store.skipCurrentMerge());
	await ready(store);
	rejectOld(new Error('Old preview failed'));
	await new Promise((resolve) => setTimeout(resolve, 10));
	expect(store.mergeCurrentSourceId).toBe(3);
	expect(store.mergePreview?.source.id).toBe(3);
	expect(store.mergeSummary.failed).toBe(0);
});

it('labels whitespace-only preview values as empty without changing actual field data', async () => {
	const { store } = await mount();
	for (const value of ['', ' ', '\t\n', null, undefined]) {
		expect(store.formatMergeValue(value)).toBe('Empty');
	}
	expect(store.formatMergeValue(0)).toBe('0');
	expect(store.formatMergeValue(' Target ')).toBe(' Target ');
});
