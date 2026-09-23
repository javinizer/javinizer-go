<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { untrack } from 'svelte';
	import { fade } from 'svelte/transition';
	import { ShieldQuestion, Check, Loader2 } from 'lucide-svelte';
	import { createQuery, createMutation, useQueryClient } from '@tanstack/svelte-query';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import type { Actress, CandidateListResponse, CandidatePromotionRequest } from '$lib/api/types';

	let {
		onPromoted = () => {}
	}: {
		onPromoted?: (actress: Actress) => void;
	} = $props();

	type Candidate = Actress & { id: number };
	type CandidateField = keyof CandidatePromotionRequest;
	type CandidateDraft = Required<CandidatePromotionRequest>;
	type CandidateDraftState = {
		values: CandidateDraft;
		dirty: Record<CandidateField, boolean>;
	};
	type CandidateValidation = { name?: boolean; thumb?: boolean };
	type PromotionInput = {
		candidate: Candidate;
		values: CandidateDraft;
		queryKey: readonly ['actress-candidates', number];
		pageOffset: number;
		pageTotal: number;
	};

	const CANDIDATE_PAGE_SIZE = 100;
	const candidateFields: CandidateField[] = [
		'first_name',
		'last_name',
		'japanese_name',
		'thumb_url'
	];

	let open = $state(false);
	let candidateOffset = $state(0);
	let drafts = $state<Record<string, CandidateDraftState>>({});
	let validationErrors = $state<Record<string, CandidateValidation>>({});
	let serverErrors = $state<Record<string, string>>({});

	const queryClient = useQueryClient();

	const candidatesQuery = createQuery(() => ({
		queryKey: ['actress-candidates', candidateOffset],
		queryFn: () => apiClient.listCandidates(CANDIDATE_PAGE_SIZE, candidateOffset),
		enabled: open
	}));

	const promoteMutation = createMutation(() => ({
		mutationFn: async ({ candidate, values }: PromotionInput) =>
			apiClient.promoteCandidate(candidate.id, values),
		onSuccess: (promoted, input) => {
			const key = candidateKey(input.candidate);
			delete drafts[key];
			delete validationErrors[key];
			delete serverErrors[key];
			queryClient.setQueryData<CandidateListResponse>(input.queryKey, (current) =>
				current
					? {
							...current,
							candidates: current.candidates.filter((item) => item.id !== input.candidate.id),
							total: Math.max(0, current.total - 1)
						}
					: current
			);
			const remaining = Math.max(0, input.pageTotal - 1);
			const lastOffset = Math.max(
				0,
				(Math.ceil(remaining / CANDIDATE_PAGE_SIZE) - 1) * CANDIDATE_PAGE_SIZE
			);
			if (candidateOffset === input.pageOffset && candidateOffset > lastOffset) {
				candidateOffset = lastOffset;
			}
			void queryClient.invalidateQueries({ queryKey: ['actress-candidates'] });
			void onPromoted(promoted);
		},
		onError: (error, input) => {
			serverErrors[candidateKey(input.candidate)] =
				error instanceof Error ? error.message : m.candidates_promotion_failed();
		}
	}));

	let fetchError = $derived(
		candidatesQuery.isError
			? candidatesQuery.error instanceof Error
				? candidatesQuery.error.message
				: m.candidates_load_failed()
			: null
	);
	let candidatesList = $derived(
		(candidatesQuery.data?.candidates ?? []).filter(hasCandidateID)
	);
	let total = $derived(candidatesQuery.data?.total ?? 0);
	let currentPage = $derived(Math.floor(candidateOffset / CANDIDATE_PAGE_SIZE) + 1);
	let totalPages = $derived(Math.max(1, Math.ceil(total / CANDIDATE_PAGE_SIZE)));
	let canGoPrev = $derived(candidateOffset > 0);
	let canGoNext = $derived(candidateOffset + CANDIDATE_PAGE_SIZE < total);
	let pending = $derived(promoteMutation.isPending);

	function hasCandidateID(candidate: Actress): candidate is Candidate {
		return Number.isSafeInteger(candidate.id) && (candidate.id ?? 0) > 0;
	}

	function candidateKey(candidate: Candidate): string {
		return String(candidate.id);
	}

	function candidateDisplayName(candidate: Candidate, draft: CandidateDraft): string {
		const japanese = draft.japanese_name.trim();
		const western = [draft.last_name.trim(), draft.first_name.trim()]
			.filter((part) => part !== '')
			.join(' ');
		return japanese || western || `#${candidate.id}`;
	}

	function initialValues(candidate: Candidate): CandidateDraft {
		return {
			first_name: candidate.first_name ?? '',
			last_name: candidate.last_name ?? '',
			japanese_name: candidate.japanese_name ?? '',
			thumb_url: candidate.thumb_url ?? ''
		};
	}

	function initialDirtyFields(): Record<CandidateField, boolean> {
		return {
			first_name: false,
			last_name: false,
			japanese_name: false,
			thumb_url: false
		};
	}

	function clearCandidateFeedback(candidate: Candidate) {
		const key = candidateKey(candidate);
		delete validationErrors[key];
		delete serverErrors[key];
		promoteMutation.reset();
	}

	function clearViewFeedback() {
		validationErrors = {};
		serverErrors = {};
		promoteMutation.reset();
	}

	function editCandidate(candidate: Candidate, field: CandidateField) {
		const key = candidateKey(candidate);
		drafts[key].dirty[field] = true;
		clearCandidateFeedback(candidate);
	}

	function isValidThumbnailURL(value: string): boolean {
		if (value.trim() === '') return true;
		try {
			const parsed = new URL(value);
			return (parsed.protocol === 'http:' || parsed.protocol === 'https:') && parsed.hostname !== '';
		} catch {
			return false;
		}
	}

	function promote(candidate: Candidate, draft: CandidateDraft) {
		const key = candidateKey(candidate);
		clearCandidateFeedback(candidate);
		const invalidName = draft.first_name.trim() === '' && draft.japanese_name.trim() === '';
		const invalidThumb = !isValidThumbnailURL(draft.thumb_url);
		if (invalidName || invalidThumb) {
			validationErrors[key] = { name: invalidName, thumb: invalidThumb };
			return;
		}
		promoteMutation.mutate({
			candidate,
			values: { ...draft },
			queryKey: ['actress-candidates', candidateOffset],
			pageOffset: candidateOffset,
			pageTotal: total
		});
	}

	function changePage(offset: number) {
		if (pending) return;
		clearViewFeedback();
		candidateOffset = offset;
	}

	async function refreshCandidates() {
		clearViewFeedback();
		await candidatesQuery.refetch();
	}

	async function toggle() {
		if (pending) return;
		clearViewFeedback();
		open = !open;
		if (open) void candidatesQuery.refetch();
	}

	$effect(() => {
		const candidates = candidatesList;
		untrack(() => {
			for (const candidate of candidates) {
				const key = candidateKey(candidate);
				const incoming = initialValues(candidate);
				const existing = drafts[key];
				if (!existing) {
					drafts[key] = { values: incoming, dirty: initialDirtyFields() };
					continue;
				}
				for (const field of candidateFields) {
					if (!existing.dirty[field]) existing.values[field] = incoming[field];
				}
			}
		});
	});
</script>

<Card class="p-5 space-y-4">
	<button
		type="button"
		class="flex w-full items-center justify-between text-left"
		aria-expanded={open}
		aria-controls="actress-candidates-list"
		disabled={pending}
		onclick={toggle}
	>
		<span class="flex items-center gap-2 font-medium">
			<ShieldQuestion class="size-4 text-muted-foreground" />
			{m.candidates_title()}
			{#if total > 0}
				<span class="rounded-full bg-amber-500/15 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
					{total}
				</span>
			{/if}
		</span>
		<span class="text-xs text-muted-foreground">{open ? m.candidates_hide() : m.candidates_review()}</span>
	</button>

	{#if open}
		<div id="actress-candidates-list" in:fade|local={{ duration: 220 }} class="space-y-3">
			<p class="text-xs text-muted-foreground">{m.candidates_description()}</p>

			{#if fetchError}
				<div class="flex items-center justify-between gap-2 rounded border border-destructive/30 bg-destructive/5 p-2">
					<p class="text-sm text-destructive" role="alert">{fetchError}</p>
					<Button variant="outline" size="sm" onclick={refreshCandidates} disabled={pending}>
						{m.common_retry()}
					</Button>
				</div>
			{/if}

			{#if candidatesQuery.isPending}
				<div class="flex items-center gap-2 text-sm text-muted-foreground">
					<Loader2 class="size-4 animate-spin" /> {m.common_loading()}
				</div>
			{:else if !fetchError && candidatesList.length === 0}
				<p class="text-sm text-muted-foreground">{m.candidates_empty()}</p>
			{:else}
				<ul class="divide-y divide-border">
					{#each candidatesList as candidate (candidate.id)}
						{@const key = candidateKey(candidate)}
						{#if drafts[key]}
							{@const draft = drafts[key].values}
							{@const errors = validationErrors[key]}
							{@const nameErrorId = `candidate-${key}-name-error`}
							{@const thumbErrorId = `candidate-${key}-thumb-error`}
							{@const serverErrorId = `candidate-${key}-server-error`}
							{@const candidatePending = pending && promoteMutation.variables?.candidate.id === candidate.id}
							<li class="grid gap-3 py-3 md:grid-cols-[1fr_auto] md:items-end">
								<fieldset
									class="grid min-w-0 gap-2 sm:grid-cols-2"
									disabled={pending}
									aria-busy={candidatePending}
								>
									<legend class="sr-only">{m.candidates_fields({ id: key })}</legend>
									<label class="grid gap-1 text-xs font-medium">
										{m.actresses_first_name()}
										<input
											class="rounded-md border border-input bg-background px-3 py-2 text-sm"
											bind:value={draft.first_name}
											oninput={() => editCandidate(candidate, 'first_name')}
											aria-invalid={errors?.name || undefined}
											aria-describedby={errors?.name ? nameErrorId : undefined}
										/>
									</label>
									<label class="grid gap-1 text-xs font-medium">
										{m.actresses_last_name()}
										<input
											class="rounded-md border border-input bg-background px-3 py-2 text-sm"
											bind:value={draft.last_name}
											oninput={() => editCandidate(candidate, 'last_name')}
										/>
									</label>
									<label class="grid gap-1 text-xs font-medium">
										{m.actresses_japanese_name()}
										<input
											class="rounded-md border border-input bg-background px-3 py-2 text-sm"
											bind:value={draft.japanese_name}
											oninput={() => editCandidate(candidate, 'japanese_name')}
											aria-invalid={errors?.name || undefined}
											aria-describedby={errors?.name ? nameErrorId : undefined}
										/>
									</label>
									<label class="grid gap-1 text-xs font-medium">
										{m.actresses_thumb_url()}
										<input
											class="rounded-md border border-input bg-background px-3 py-2 text-sm"
											type="url"
											bind:value={draft.thumb_url}
											oninput={() => editCandidate(candidate, 'thumb_url')}
											aria-invalid={errors?.thumb || undefined}
											aria-describedby={errors?.thumb ? thumbErrorId : undefined}
										/>
									</label>
									{#if errors?.name}
										<p id={nameErrorId} class="text-xs text-destructive sm:col-span-2" role="alert">
											{m.actresses_name_required()}
										</p>
									{/if}
									{#if errors?.thumb}
										<p id={thumbErrorId} class="text-xs text-destructive sm:col-span-2" role="alert">
											{m.candidates_thumb_url_invalid()}
										</p>
									{/if}
									{#if serverErrors[key]}
										<p id={serverErrorId} class="text-xs text-destructive sm:col-span-2" role="alert">
											{serverErrors[key]}
										</p>
									{/if}
									{#if candidatePending}
										<p class="sr-only" role="status">{m.candidates_promoting()}</p>
									{/if}
									{#if candidate.dmm_id}
										<p class="text-xs text-muted-foreground sm:col-span-2">DMM {candidate.dmm_id}</p>
									{/if}
								</fieldset>
								<Button
									variant="outline"
									size="sm"
									onclick={() => promote(candidate, draft)}
									disabled={pending}
									aria-label={m.candidates_promote_for({ name: candidateDisplayName(candidate, draft) })}
								>
									{#if candidatePending}
										<Loader2 class="size-3.5 animate-spin" />
									{:else}
										<Check class="size-3.5" />
									{/if}
									{m.candidates_promote()}
								</Button>
							</li>
						{/if}
					{/each}
				</ul>

				{#if totalPages > 1}
					<div class="flex items-center justify-between gap-3 border-t pt-3">
						<span class="text-xs text-muted-foreground">{m.actresses_page_of({ current: currentPage, total: totalPages })}</span>
						<div class="flex items-center gap-2">
							<Button
								variant="outline"
								size="sm"
								onclick={() => changePage(candidateOffset - CANDIDATE_PAGE_SIZE)}
								disabled={!canGoPrev || pending}
							>
								{m.actresses_prev()}
							</Button>
							<Button
								variant="outline"
								size="sm"
								onclick={() => changePage(candidateOffset + CANDIDATE_PAGE_SIZE)}
								disabled={!canGoNext || pending}
							>
								{m.actresses_next()}
							</Button>
						</div>
					</div>
				{/if}
			{/if}
		</div>
	{/if}
</Card>
