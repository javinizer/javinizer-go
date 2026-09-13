<script lang="ts">
	import { fade } from 'svelte/transition';
	import { ShieldQuestion, Check, Loader2 } from 'lucide-svelte';
	import { createQuery, createMutation, useQueryClient } from '@tanstack/svelte-query';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import type { Actress } from '$lib/api/types';

	let {
		onPromoted = () => {}
	}: {
		onPromoted?: (actress: Actress) => void;
	} = $props();

	let open = $state(false);

	const queryClient = useQueryClient();

	const candidatesQuery = createQuery(() => ({
		queryKey: ['actress-candidates'],
		queryFn: () => apiClient.listCandidates(100, 0),
		enabled: open
	}));

	const promoteMutation = createMutation(() => ({
		mutationFn: async (candidate: Actress) =>
			apiClient.promoteCandidate(candidate.id ?? 0, {
				first_name: candidate.first_name,
				last_name: candidate.last_name,
				japanese_name: candidate.japanese_name,
				thumb_url: candidate.thumb_url
			}),
		onSuccess: (promoted) => {
			void queryClient.invalidateQueries({ queryKey: ['actress-candidates'] });
			void onPromoted(promoted);
		}
	}));

	let fetchError = $derived(
		candidatesQuery.isError
			? candidatesQuery.error instanceof Error
				? candidatesQuery.error.message
				: 'Failed to load candidates'
			: null
	);
	let promoteError = $derived(
		promoteMutation.isError
			? promoteMutation.error instanceof Error
				? promoteMutation.error.message
				: 'Promotion failed'
			: null
	);

	async function toggle() {
		open = !open;
		if (open) void candidatesQuery.refetch();
	}

	let candidatesList = $derived(candidatesQuery.data?.candidates ?? []);
	let total = $derived(candidatesQuery.data?.total ?? 0);
</script>

<Card class="p-5 space-y-4">
	<button
		type="button"
		class="flex w-full items-center justify-between text-left"
		aria-expanded={open}
		aria-controls="actress-candidates-list"
		onclick={toggle}
	>
		<span class="flex items-center gap-2 font-medium">
			<ShieldQuestion class="size-4 text-muted-foreground" />
			Candidate identities
			{#if total > 0}
				<span class="rounded-full bg-amber-500/15 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
					{total}
				</span>
			{/if}
		</span>
		<span class="text-xs text-muted-foreground">{open ? "Hide" : "Review"}</span>
	</button>

	{#if open}
		<div id="actress-candidates-list" in:fade|local={{ duration: 220 }} class="space-y-3">
			<p class="text-xs text-muted-foreground">
				Quarantined identities created by scrapes that could not be matched to your catalog.
				They are excluded from enrichment and rendering until promoted.
			</p>

			{#if promoteError}
				<p class="text-sm text-destructive">{promoteError}</p>
			{/if}

			{#if fetchError}
				<div class="flex items-center justify-between gap-2 rounded border border-destructive/30 bg-destructive/5 p-2">
					<p class="text-sm text-destructive">{fetchError}</p>
					<Button variant="outline" size="sm" onclick={() => void candidatesQuery.refetch()}>
						Retry
					</Button>
				</div>
			{/if}

			{#if candidatesQuery.isFetching}
				<div class="flex items-center gap-2 text-sm text-muted-foreground">
					<Loader2 class="size-4 animate-spin" /> Loading…
				</div>
			{:else if !fetchError && candidatesList.length === 0}
				<p class="text-sm text-muted-foreground">No candidates. Your catalog is clean.</p>
			{:else}
				<ul class="divide-y divide-border">
					{#each candidatesList as candidate (candidate.id)}
						<li class="flex items-center justify-between gap-3 py-2">
							<div class="min-w-0">
								<p class="truncate text-sm font-medium">
									{candidate.japanese_name || candidate.first_name + " " + candidate.last_name}
								</p>
								<p class="truncate text-xs text-muted-foreground">
									{candidate.thumb_url || "no thumb"}
									{#if candidate.dmm_id}
										· dmm {candidate.dmm_id}
									{/if}
								</p>
							</div>
							<Button
								variant="outline"
								size="sm"
								onclick={() => promoteMutation.mutate(candidate)}
								disabled={promoteMutation.isPending}
							>
								{#if promoteMutation.isPending && promoteMutation.variables?.id === candidate.id}
									<Loader2 class="size-3.5 animate-spin" />
								{:else}
									<Check class="size-3.5" />
								{/if}
								Promote
							</Button>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	{/if}
</Card>
