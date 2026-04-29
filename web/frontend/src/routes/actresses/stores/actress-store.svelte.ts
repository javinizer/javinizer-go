import { untrack } from 'svelte';
import {
	createQuery,
	createMutation,
	useQueryClient
} from '@tanstack/svelte-query';
import { apiClient } from '$lib/api/client';
import { confirmDialog } from '$lib/stores/dialog.svelte';
import { toastStore } from '$lib/stores/toast';
import type {
	Actress,
	ActressUpsertRequest,
	ActressMergeResolution
} from '$lib/api/types';

export type ActressForm = {
	dmm_id: string;
	first_name: string;
	last_name: string;
	japanese_name: string;
	thumb_url: string;
	aliases: string;
};

const DEFAULT_LIMIT = 20;

export function createActressStore() {
	const queryClient = useQueryClient();

	let queryInput = $state('');
	let activeQuery = $state('');
	let limit = $state(DEFAULT_LIMIT);
	let offset = $state(0);
	let viewMode = $state<'cards' | 'compact' | 'table'>('cards');
	let sortBy = $state<'name' | 'japanese_name' | 'id' | 'dmm_id' | 'updated_at' | 'created_at'>('name');
	let sortOrder = $state<'asc' | 'desc'>('asc');

	let editingId = $state<number | null>(null);
	let form = $state<ActressForm>(emptyForm());
	let formError = $state<string | null>(null);
	let selectedIds = $state<number[]>([]);

	const actressesQuery = createQuery(() => ({
		queryKey: ['actresses', { limit, offset, q: activeQuery, sort_by: sortBy, sort_order: sortOrder }],
		queryFn: () => apiClient.listActresses({
			limit,
			offset,
			q: activeQuery || undefined,
			sort_by: sortBy,
			sort_order: sortOrder
		}),
		placeholderData: (prev) => prev
	}));

	let actresses = $derived(actressesQuery.data?.actresses ?? []);
	let total = $derived(actressesQuery.data?.total ?? 0);
	let loading = $derived(actressesQuery.isPending && !actressesQuery.data);
	let error = $derived(actressesQuery.error?.message ?? null);
	let isRefreshing = $derived(actressesQuery.isFetching && !!actressesQuery.data);

	const saveActressMutation = createMutation(() => ({
		mutationFn: async (payload: ActressUpsertRequest) => {
			if (editingId !== null) {
				return apiClient.updateActress(editingId, payload);
			}
			return apiClient.createActress(payload);
		},
		onSuccess: () => {
			if (editingId !== null) {
				toastStore.success('Actress updated');
			} else {
				toastStore.success('Actress created');
			}
			queryClient.invalidateQueries({ queryKey: ['actresses'] });
			resetForm();
		},
		onError: (err: Error) => {
			formError = err.message;
		}
	}));

	const deleteActressMutation = createMutation(() => ({
		mutationFn: (id: number) => apiClient.deleteActress(id),
		onSuccess: (_data, id) => {
			toastStore.success('Actress deleted');
			selectedIds = selectedIds.filter((sid) => sid !== id);
			const currentActresses = actressesQuery.data?.actresses ?? [];
			if (currentActresses.length === 1 && offset > 0) {
				offset = Math.max(0, offset - limit);
			}
			if (editingId === id) {
				resetForm();
			}
			queryClient.invalidateQueries({ queryKey: ['actresses'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message);
		}
	}));

	$effect(() => {
		const data = actressesQuery.data;
		if (!data || showMergeModal) return;
		const pageIDs = new Set(
			data.actresses.map((actress) => actress.id).filter((id): id is number => id !== undefined)
		);
		untrack(() => {
			const pruned = selectedIds.filter((id) => pageIDs.has(id));
			if (pruned.length !== selectedIds.length) {
				selectedIds = pruned;
			}
		});
	});

	let showMergeModal = $state(false);
	let mergePrimaryId = $state<number | null>(null);
	let mergeSourceQueue = $state<number[]>([]);
	let mergeCurrentSourceId = $state<number | null>(null);
	let mergeResolutions = $state<Record<string, ActressMergeResolution>>({});
	let mergeSummary = $state<{ success: number; failed: number; messages: string[] }>({
		success: 0,
		failed: 0,
		messages: []
	});

	const mergePreviewQuery = createQuery(() => ({
		queryKey: ['actress-merge-preview', mergePrimaryId, mergeCurrentSourceId],
		queryFn: () => apiClient.previewActressMerge({
			target_id: mergePrimaryId!,
			source_id: mergeCurrentSourceId!
		}),
		enabled: !!mergePrimaryId && !!mergeCurrentSourceId
	}));

	let mergePreview = $derived(mergePreviewQuery.data ?? null);

	const mergeActressMutation = createMutation(() => ({
		mutationFn: (params: { target_id: number; source_id: number; resolutions: Record<string, ActressMergeResolution> }) =>
			apiClient.mergeActresses(params),
		onSuccess: (result, variables) => {
			mergeSummary = {
				...mergeSummary,
				success: mergeSummary.success + 1,
				messages: [...mergeSummary.messages, `Merged #${result.merged_from_id} into #${variables.target_id}`]
			};
			selectedIds = selectedIds.filter((id) => id !== variables.source_id);
			mergeSourceQueue = mergeSourceQueue.slice(1);
			queryClient.invalidateQueries({ queryKey: ['actresses'] });
			queryClient.invalidateQueries({ queryKey: ['actress-merge-preview'] });
			advanceMergeQueue();
		},
		onError: (err: Error, variables) => {
			mergeSummary = {
				...mergeSummary,
				failed: mergeSummary.failed + 1,
				messages: [...mergeSummary.messages, `Failed #${variables.source_id}: ${err.message}`]
			};
			mergeSourceQueue = mergeSourceQueue.slice(1);
			queryClient.invalidateQueries({ queryKey: ['actress-merge-preview'] });
			advanceMergeQueue();
		}
	}));

	$effect(() => {
		const data = mergePreviewQuery.data;
		if (data) {
			untrack(() => {
				mergeResolutions = { ...data.default_resolutions };
			});
		}
	});

	let lastPreviewErrorSourceId = $state<number | null>(null);

	$effect(() => {
		if (mergePreviewQuery.isError && mergeCurrentSourceId && mergeCurrentSourceId !== lastPreviewErrorSourceId) {
			lastPreviewErrorSourceId = mergeCurrentSourceId;
			const message = mergePreviewQuery.error?.message ?? 'Failed to preview merge';
			untrack(() => {
				mergeSummary = {
					...mergeSummary,
					failed: mergeSummary.failed + 1,
					messages: [...mergeSummary.messages, `Skipped #${mergeCurrentSourceId}: ${message}`]
				};
				mergeSourceQueue = mergeSourceQueue.slice(1);
			});
			advanceMergeQueue();
		}
	});

	const currentPage = $derived(Math.floor(offset / limit) + 1);
	const totalPages = $derived(Math.max(1, Math.ceil(total / limit)));
	const canGoPrev = $derived(offset > 0);
	const canGoNext = $derived(offset + limit < total);

	function itemDelay(index: number): number {
		return Math.min(index * 35, 280);
	}

	function emptyForm(): ActressForm {
		return {
			dmm_id: '',
			first_name: '',
			last_name: '',
			japanese_name: '',
			thumb_url: '',
			aliases: ''
		};
	}

	function getDisplayName(actress: Actress): string {
		if (actress.last_name && actress.first_name) return `${actress.last_name} ${actress.first_name}`;
		if (actress.first_name) return actress.first_name;
		if (actress.japanese_name) return actress.japanese_name;
		return 'Unnamed';
	}

	function getActressLabelByID(id: number): string {
		const actress = actresses.find((item) => item.id === id);
		if (!actress) return `Actress #${id}`;
		return `#${id} - ${getDisplayName(actress)}`;
	}

	function isSelected(actress: Actress): boolean {
		return actress.id !== undefined && selectedIds.includes(actress.id);
	}

	function toggleSelection(actress: Actress) {
		if (!actress.id) return;
		if (selectedIds.includes(actress.id)) {
			selectedIds = selectedIds.filter((id) => id !== actress.id);
			return;
		}
		selectedIds = [...selectedIds, actress.id];
	}

	function selectCurrentPage() {
		const ids = actresses.map((actress) => actress.id).filter((id): id is number => id !== undefined);
		selectedIds = [...new Set([...selectedIds, ...ids])];
	}

	function clearSelection() {
		selectedIds = [];
	}

	function toPayload(): ActressUpsertRequest {
		const dmmID = Number.parseInt(form.dmm_id, 10);
		return {
			dmm_id: Number.isNaN(dmmID) ? undefined : dmmID,
			first_name: form.first_name.trim(),
			last_name: form.last_name.trim(),
			japanese_name: form.japanese_name.trim(),
			thumb_url: form.thumb_url.trim(),
			aliases: form.aliases.trim()
		};
	}

	function validateForm(): string | null {
		const payload = toPayload();
		if (payload.dmm_id !== undefined && payload.dmm_id < 0) {
			return 'DMM ID must be greater than or equal to 0';
		}
		if (!payload.first_name && !payload.japanese_name) {
			return 'Either First Name or Japanese Name is required';
		}
		return null;
	}

	function resetForm() {
		editingId = null;
		form = emptyForm();
		formError = null;
	}

	function startEdit(actress: Actress) {
		editingId = actress.id ?? null;
		form = {
			dmm_id: actress.dmm_id && actress.dmm_id > 0 ? actress.dmm_id.toString() : '',
			first_name: actress.first_name ?? '',
			last_name: actress.last_name ?? '',
			japanese_name: actress.japanese_name ?? '',
			thumb_url: actress.thumb_url ?? '',
			aliases: actress.aliases ?? ''
		};
		formError = null;
	}

	function saveActress() {
		formError = validateForm();
		if (formError) return;
		saveActressMutation.mutate(toPayload());
	}

	async function removeActress(actress: Actress) {
		if (!actress.id) return;
		const name = getDisplayName(actress);
		if (!(await confirmDialog('Delete Actress', `Delete actress "${name}"?`, { variant: 'danger', confirmLabel: 'Delete' }))) return;
		deleteActressMutation.mutate(actress.id);
	}

	function applySearch() {
		activeQuery = queryInput.trim();
		offset = 0;
	}

	function clearSearch() {
		queryInput = '';
		activeQuery = '';
		offset = 0;
	}

	function applySort() {
		offset = 0;
	}

	function toggleSortOrder() {
		sortOrder = sortOrder === 'asc' ? 'desc' : 'asc';
		applySort();
	}

	function prevPage() {
		if (!canGoPrev) return;
		offset = Math.max(0, offset - limit);
	}

	function nextPage() {
		if (!canGoNext) return;
		offset += limit;
	}

	function resetMergeState() {
		mergeSourceQueue = [];
		mergeCurrentSourceId = null;
		mergeResolutions = {};
		lastPreviewErrorSourceId = null;
		mergeSummary = { success: 0, failed: 0, messages: [] };
	}

	function closeMergeModal() {
		showMergeModal = false;
		mergePrimaryId = null;
		resetMergeState();
	}

	function startMergeSelected() {
		if (selectedIds.length < 2) {
			toastStore.warning('Select at least 2 actresses to merge');
			return;
		}
		mergePrimaryId = selectedIds[0];
		showMergeModal = true;
		resetMergeState();
		resetMergeQueueAndPreview();
	}

	function resetMergeQueueAndPreview() {
		if (!mergePrimaryId) return;
		mergeSourceQueue = selectedIds.filter((id) => id !== mergePrimaryId);
		advanceMergeQueue();
	}

	function formatMergeValue(value: unknown): string {
		if (value === null || value === undefined || value === '') return 'Empty';
		return String(value);
	}

	function setResolution(field: string, decision: ActressMergeResolution) {
		mergeResolutions = { ...mergeResolutions, [field]: decision };
	}

	function advanceMergeQueue() {
		if (!mergePrimaryId || mergeSourceQueue.length === 0) {
			mergeCurrentSourceId = null;
			return;
		}
		lastPreviewErrorSourceId = null;
		mergeCurrentSourceId = mergeSourceQueue[0];
	}

	function applyCurrentMerge() {
		if (!mergePrimaryId || !mergeCurrentSourceId || !mergePreviewQuery.data || mergeActressMutation.isPending) return;
		mergeActressMutation.mutate({
			target_id: mergePrimaryId,
			source_id: mergeCurrentSourceId,
			resolutions: mergeResolutions
		});
	}

	function skipCurrentMerge() {
		if (!mergeCurrentSourceId || mergeActressMutation.isPending) return;
		mergeSummary = {
			...mergeSummary,
			messages: [...mergeSummary.messages, `Skipped #${mergeCurrentSourceId}`]
		};
		mergeSourceQueue = mergeSourceQueue.slice(1);
		advanceMergeQueue();
	}

	function refresh() {
		queryClient.invalidateQueries({ queryKey: ['actresses'] });
	}

	return {
		get queryInput() { return queryInput; },
		set queryInput(v: string) { queryInput = v; },
		get activeQuery() { return activeQuery; },
		get limit() { return limit; },
		get offset() { return offset; },
		get viewMode() { return viewMode; },
		set viewMode(v: 'cards' | 'compact' | 'table') { viewMode = v; },
		get sortBy() { return sortBy; },
		set sortBy(v: string) { sortBy = v as typeof sortBy; },
		get sortOrder() { return sortOrder; },
		get editingId() { return editingId; },
		get form() { return form; },
		set form(v: ActressForm) { form = v; },
		get formError() { return formError; },
		get selectedIds() { return selectedIds; },
		get actresses() { return actresses; },
		get total() { return total; },
		get loading() { return loading; },
		get error() { return error; },
		get isRefreshing() { return isRefreshing; },
		get saveActressMutation() { return saveActressMutation; },
		get deleteActressMutation() { return deleteActressMutation; },
		get showMergeModal() { return showMergeModal; },
		set showMergeModal(v: boolean) { showMergeModal = v; },
		get mergePrimaryId() { return mergePrimaryId; },
		set mergePrimaryId(v: number | null) { mergePrimaryId = v; },
		get mergeSourceQueue() { return mergeSourceQueue; },
		get mergeCurrentSourceId() { return mergeCurrentSourceId; },
		get mergeResolutions() { return mergeResolutions; },
		set mergeResolutions(v: Record<string, ActressMergeResolution>) { mergeResolutions = v; },
		get mergeSummary() { return mergeSummary; },
		get mergePreview() { return mergePreview; },
		get mergePreviewQuery() { return mergePreviewQuery; },
		get mergeActressMutation() { return mergeActressMutation; },
		get currentPage() { return currentPage; },
		get totalPages() { return totalPages; },
		get canGoPrev() { return canGoPrev; },
		get canGoNext() { return canGoNext; },
		itemDelay,
		emptyForm,
		getDisplayName,
		getActressLabelByID,
		isSelected,
		toggleSelection,
		selectCurrentPage,
		clearSelection,
		resetForm,
		startEdit,
		saveActress,
		removeActress,
		applySearch,
		clearSearch,
		toggleSortOrder,
		prevPage,
		nextPage,
		closeMergeModal,
		startMergeSelected,
		resetMergeQueueAndPreview,
		formatMergeValue,
		setResolution,
		applyCurrentMerge,
		skipCurrentMerge,
		refresh
	};
}
