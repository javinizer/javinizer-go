<script lang="ts">
	import { fade } from 'svelte/transition';
	import { createQuery, createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { TriangleAlert, Loader2, Tag, Unlink } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import type { CreditCollision, CollisionResolution } from '$lib/api/types';

	let {
		movieContentId,
		onResolved = () => {}
	}: {
		movieContentId: string;
		onResolved?: (remaining: number) => void;
	} = $props();

	const queryClient = useQueryClient();

	const collisionsQuery = createQuery(() => ({
		queryKey: ['collisions', movieContentId],
		queryFn: () => apiClient.listCollisions(movieContentId),
		enabled: !!movieContentId
	}));

	let openCollisions = $derived(collisionsQuery.data?.collisions ?? []);

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
		onSuccess: (res, variables) => {
			void queryClient.invalidateQueries({ queryKey: ['collisions'] });
			if (variables.movieContentId === movieContentId) {
				onResolved(res.remaining_open);
			}
		}
	}));

	let resolveError = $derived(
		resolveMutation.isError &&
		resolveMutation.variables?.movieContentId === movieContentId
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

	function resolve(collision: CreditCollision, resolution: CollisionResolution) {
		if (resolveMutation.isPending) return;
		if (resolution === 'reassign') {
			const input = prompt('Target actress ID to relink this credit to:');
			const parsed = Number(input);
			if (!Number.isSafeInteger(parsed) || parsed <= 0) {
				alert('Relink needs a positive actress ID.');
				return;
			}
			resolveMutation.mutate({ collision, resolution, targetActressId: parsed, movieContentId });
			return;
		}
		resolveMutation.mutate({ collision, resolution, movieContentId });
	}

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
						<div class="flex flex-wrap gap-2 pt-1">
							<Button
								variant="outline"
								size="sm"
								disabled={resolveMutation.isPending}
								onclick={() => resolve(collision, 'keep_identity')}
							>
								{#if pendingCollisionId === collision.id}<Loader2 class="size-3.5 animate-spin" />{/if}
								<Unlink class="size-3.5" /> Keep catalog
							</Button>
							<Button
								variant="outline"
								size="sm"
								disabled={resolveMutation.isPending}
								onclick={() => resolve(collision, 'adopt_canonical')}
							>
								{#if pendingCollisionId === collision.id}<Loader2 class="size-3.5 animate-spin" />{/if}
								<Tag class="size-3.5" /> Adopt as truth
							</Button>
							{#if collision.field !== 'identity_link'}
								<Button
									variant="outline"
									size="sm"
									disabled={resolveMutation.isPending}
									onclick={() => resolve(collision, 'adopt_alias')}
								>
									Alias
								</Button>
							{/if}
							<Button
								variant="outline"
								size="sm"
								disabled={resolveMutation.isPending}
								onclick={() => resolve(collision, 'reassign')}
							>
								Relink
							</Button>
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</Card>
{/if}
