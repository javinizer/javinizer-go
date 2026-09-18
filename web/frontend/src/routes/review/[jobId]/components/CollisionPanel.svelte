<script lang="ts">
	import { tick } from 'svelte';
	import { fade } from 'svelte/transition';
	import { createQuery, createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { TriangleAlert, Loader2, Search, Tag, Unlink, X } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import { ApiError } from '$lib/api/clients/common';
	import type { Actress, CreditCollision, CollisionResolution } from '$lib/api/types';
	import { formatActressName } from '$lib/utils/actress';

	let {
		movieContentId,
		onResolved = () => {}
	}: {
		movieContentId: string;
		onResolved?: (remaining: number, movieContentId: string) => void | Promise<void>;
	} = $props();

	const queryClient = useQueryClient();

	const collisionsQuery = createQuery(() => ({
		queryKey: ['collisions', movieContentId],
		queryFn: () => apiClient.listCollisions(movieContentId),
		enabled: !!movieContentId
	}));

	let openCollisions = $derived(collisionsQuery.data?.collisions ?? []);
	let reconciledConflict = $state<{ collisionId: number; movieContentId: string } | null>(null);

	const resolveMutation = createMutation(() => ({
		mutationFn: async (input: {
			collision: CreditCollision;
			resolution: CollisionResolution;
			targetActressId?: number;
			movieContentId: string;
		}) =>
			apiClient.resolveCollision(input.collision.id, {
				resolution: input.resolution,
				target_actress_id: input.targetActressId
			}),
		onSuccess: async (res, variables) => {
			if (variables.resolution === 'reassign' && relinkCollision?.id === variables.collision.id) {
				closeRelink();
			}
			await queryClient.refetchQueries({
				queryKey: ['collisions', variables.movieContentId],
				exact: true,
				type: 'active'
			});
			if (variables.movieContentId === movieContentId) {
				await onResolved(res.remaining_open, variables.movieContentId);
			}
		},
		onError: async (error, variables) => {
			if (!(error instanceof ApiError) || error.status !== 409) return;
			const queryKey = ['collisions', variables.movieContentId] as const;
			await queryClient.refetchQueries({ queryKey, exact: true, type: 'active' });
			const refreshed = queryClient.getQueryData<{ collisions: CreditCollision[] }>(queryKey);
			const collisionGone =
				refreshed !== undefined &&
				!refreshed.collisions.some((collision) => collision.id === variables.collision.id);
			if (collisionGone) {
				reconciledConflict = {
					collisionId: variables.collision.id,
					movieContentId: variables.movieContentId
				};
				if (
					relinkCollision?.id === variables.collision.id &&
					relinkMovieContentId === variables.movieContentId
				) {
					closeRelink();
				}
			}
			if (variables.movieContentId !== movieContentId) return;
			await onResolved(refreshed?.collisions.length ?? 0, variables.movieContentId);
		}
	}));

	let resolveError = $derived(
		resolveMutation.isError &&
		resolveMutation.variables?.movieContentId === movieContentId &&
		!(
			reconciledConflict?.collisionId === resolveMutation.variables?.collision.id &&
			reconciledConflict?.movieContentId === resolveMutation.variables?.movieContentId
		)
			? resolveMutation.error instanceof Error
				? resolveMutation.error.message
				: 'Resolution failed'
			: null
	);
	let queryError = $derived(
		collisionsQuery.isError
			? collisionsQuery.error instanceof Error
				? collisionsQuery.error.message
				: 'Failed to load collisions'
			: null
	);
	let pendingCollisionId = $derived(
		resolveMutation.isPending ? (resolveMutation.variables?.collision.id ?? null) : null
	);

	let relinkCollision = $state<CreditCollision | null>(null);
	let relinkMovieContentId = $state('');
	let searchQuery = $state('');
	let searchResults = $state<Actress[]>([]);
	let selectedTarget = $state<Actress | null>(null);
	let searchLoading = $state(false);
	let searchError = $state<string | null>(null);
	let searchInput = $state<HTMLInputElement | null>(null);
	let searchRequestId = 0;

	function closeRelink() {
		searchRequestId += 1;
		relinkCollision = null;
		relinkMovieContentId = '';
		searchQuery = '';
		searchResults = [];
		selectedTarget = null;
		searchLoading = false;
		searchError = null;
	}

	async function searchVerifiedTargets(query: string) {
		const collision = relinkCollision;
		const requestMovie = relinkMovieContentId;
		if (!collision || requestMovie !== movieContentId) return;
		const requestId = ++searchRequestId;
		searchLoading = true;
		searchError = null;
		try {
			const results = await apiClient.searchActresses(query.trim());
			if (requestId !== searchRequestId || collision.id !== relinkCollision?.id || requestMovie !== movieContentId) return;
			searchResults = results.filter(
				(actress) => actress.verified === true && actress.id !== collision.current_actress_id,
			);
		} catch (error) {
			if (requestId === searchRequestId) {
				searchError = error instanceof Error ? error.message : 'Actress search failed';
				searchResults = [];
			}
		} finally {
			if (requestId === searchRequestId) searchLoading = false;
		}
	}

	async function openRelink(collision: CreditCollision) {
		if (resolveMutation.isPending) return;
		relinkCollision = collision;
		relinkMovieContentId = movieContentId;
		searchQuery = '';
		selectedTarget = null;
		await tick();
		searchInput?.focus();
		await searchVerifiedTargets('');
	}

	function confirmRelink() {
		const collision = relinkCollision;
		const target = selectedTarget;
		if (!collision || !target || relinkMovieContentId !== movieContentId) return;
		if (target.verified !== true || target.id === collision.current_actress_id) return;
		reconciledConflict = null;
		resolveMutation.mutate({
			collision,
			resolution: 'reassign',
			targetActressId: target.id,
			movieContentId: relinkMovieContentId,
		});
	}

	function resolve(collision: CreditCollision, resolution: CollisionResolution) {
		if (resolveMutation.isPending) return;
		if (resolution === 'reassign') {
			void openRelink(collision);
			return;
		}
		reconciledConflict = null;
		resolveMutation.mutate({ collision, resolution, movieContentId });
	}

	$effect(() => {
		if (relinkCollision && relinkMovieContentId !== movieContentId) closeRelink();
	});

	const fieldLabel: Record<string, string> = {
		credited_name: 'Name',
		reported_thumb_url: 'Thumb',
		identity_link: 'Identity link'
	};
</script>

{#if openCollisions.length > 0 || resolveError || queryError || collisionsQuery.isFetching}
	<Card class="border-destructive/40 p-5 space-y-3">
		<div class="flex items-center gap-2 font-medium">
			<TriangleAlert class="size-4 text-destructive" />
			Collisions — organize is blocked until resolved
			<span class="rounded-full bg-destructive/15 px-2 py-0.5 text-xs text-destructive">
				{openCollisions.length}
			</span>
		</div>

		<p class="text-xs text-muted-foreground">
			Scraped data conflicts with your catalog. Resolve each collision to unblock organizing
			this movie.
		</p>

		{#if resolveError}
			<p class="text-sm text-destructive">{resolveError}</p>
		{/if}

		{#if queryError}
			<div class="flex items-center justify-between gap-2 rounded border border-destructive/30 bg-destructive/5 p-2">
				<p class="text-sm text-destructive">{queryError}</p>
				<Button variant="outline" size="sm" onclick={() => void collisionsQuery.refetch()}>
					Retry
				</Button>
			</div>
		{/if}

		{#if collisionsQuery.isFetching}
			<div class="flex items-center gap-2 text-sm text-muted-foreground">
				<Loader2 class="size-4 animate-spin" /> Loading collisions…
			</div>
		{:else}
			<ul class="space-y-3">
				{#each openCollisions as collision (collision.id)}
					<li in:fade|local={{ duration: 160 }} class="rounded-lg border border-border/70 p-3 space-y-2">
						<div class="flex items-center justify-between gap-2 text-xs">
							<span class="font-medium">{fieldLabel[collision.field] ?? collision.field}</span>
							<span class="text-muted-foreground">
								{#if collision.occurrences > 1}seen {collision.occurrences}× · {/if}
								{collision.sources_seen || 'unknown source'}
							</span>
						</div>
						<div class="grid grid-cols-2 gap-2 text-xs">
							<div class="rounded bg-muted/50 p-2">
								<p class="text-muted-foreground">Scrape reported</p>
								<p class="truncate font-medium">{collision.reported_value || '—'}</p>
							</div>
							<div class="rounded bg-muted/30 p-2">
								<p class="text-muted-foreground">Catalog says</p>
								<p class="truncate font-medium">{collision.canonical_value || '—'}</p>
							</div>
						</div>
						<div class="flex flex-wrap gap-2 pt-1" aria-label="Available collision resolutions">
							{#if collision.allowed_resolutions.includes('keep_identity')}
								<Button
									variant="outline"
									size="sm"
									disabled={resolveMutation.isPending}
									onclick={() => resolve(collision, 'keep_identity')}
								>
									{#if pendingCollisionId === collision.id}<Loader2 class="size-3.5 animate-spin" />{/if}
									<Unlink class="size-3.5" /> Keep catalog
								</Button>
							{/if}
							{#if collision.allowed_resolutions.includes('adopt_canonical')}
								<Button
									variant="outline"
									size="sm"
									disabled={resolveMutation.isPending}
									onclick={() => resolve(collision, 'adopt_canonical')}
								>
									{#if pendingCollisionId === collision.id}<Loader2 class="size-3.5 animate-spin" />{/if}
									<Tag class="size-3.5" /> Adopt as truth
								</Button>
							{/if}
							{#if collision.allowed_resolutions.includes('adopt_alias')}
								<Button
									variant="outline"
									size="sm"
									disabled={resolveMutation.isPending}
									onclick={() => resolve(collision, 'adopt_alias')}
								>
									Alias
								</Button>
							{/if}
							{#if collision.allowed_resolutions.includes('reassign')}
								<Button
									variant="outline"
									size="sm"
									disabled={resolveMutation.isPending}
									onclick={() => resolve(collision, 'reassign')}
								>
									Relink
								</Button>
							{/if}
						</div>
						{#if relinkCollision?.id === collision.id && relinkMovieContentId === movieContentId}
							<div
								role="dialog"
								tabindex="-1"
								aria-label="Choose verified actress to relink"
								class="space-y-3 rounded-md border bg-background p-3"
								onkeydown={(event) => {
									if (event.key === 'Escape') closeRelink();
								}}
							>
								<div class="flex items-center justify-between gap-2">
									<p class="text-sm font-medium">Relink to a verified actress</p>
									<Button variant="ghost" size="icon" aria-label="Cancel relink" onclick={closeRelink}>
										<X class="size-4" />
									</Button>
								</div>
								<form
									class="flex flex-col gap-2 sm:flex-row"
									onsubmit={(event) => {
										event.preventDefault();
										void searchVerifiedTargets(searchQuery);
									}}
								>
									<label class="sr-only" for={`relink-search-${collision.id}`}>Search verified actresses</label>
									<input
										id={`relink-search-${collision.id}`}
										bind:this={searchInput}
										bind:value={searchQuery}
										class="min-w-0 flex-1 rounded-md border border-input bg-background px-3 py-2 text-sm"
										placeholder="Search by actress name"
									/>
									<Button type="submit" variant="outline" size="sm" disabled={searchLoading}>
										{#if searchLoading}<Loader2 class="size-4 animate-spin" />{:else}<Search class="size-4" />{/if}
										Search
									</Button>
								</form>
								{#if searchError}<p class="text-sm text-destructive">{searchError}</p>{/if}
								{#if selectedTarget}
									<p class="rounded border border-primary/40 bg-primary/5 px-3 py-2 text-sm" aria-live="polite">
										Selected target: {formatActressName(selectedTarget)} #{selectedTarget.id}
									</p>
								{/if}
								{#if !searchLoading && !searchError && searchResults.length === 0}
									<p class="text-sm text-muted-foreground">No eligible verified actresses found.</p>
								{:else if searchResults.length > 0}
									<ul class="max-h-48 space-y-1 overflow-y-auto" aria-label="Verified actress search results">
										{#each searchResults as actress (actress.id)}
											<li>
												<button
													type="button"
													aria-pressed={selectedTarget?.id === actress.id}
													class="w-full rounded border px-3 py-2 text-left text-sm hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
													onclick={() => (selectedTarget = actress)}
												>
													{formatActressName(actress)} <span class="text-muted-foreground">#{actress.id}</span>
												</button>
											</li>
										{/each}
									</ul>
								{/if}
								<div class="flex flex-wrap justify-end gap-2">
									<Button variant="outline" size="sm" onclick={closeRelink} disabled={resolveMutation.isPending}>Cancel</Button>
									<Button size="sm" onclick={confirmRelink} disabled={!selectedTarget || resolveMutation.isPending}>
										{#if resolveMutation.isPending}<Loader2 class="size-4 animate-spin" />{/if}
										Confirm relink
									</Button>
								</div>
							</div>
						{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</Card>
{/if}
