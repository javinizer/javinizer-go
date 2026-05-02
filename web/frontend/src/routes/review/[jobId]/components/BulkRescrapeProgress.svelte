<script lang="ts">
	import { fade, fly } from 'svelte/transition';
	import { Check, LoaderCircle, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';

	interface Props {
		progress: { movie_id: string; status: string; error?: string }[];
		active: boolean;
		onDismiss?: () => void;
	}

	let {
		progress,
		active,
		onDismiss
	}: Props = $props();

	let dismissed = $state(false);

	$effect(() => {
		if (active) {
			dismissed = false;
		}
	});

	$effect(() => {
		if (!active && progress.length > 0) {
			const allDone = progress.every(p => p.status === 'success' || p.status === 'failed');
			if (allDone) {
				const hasFailures = progress.some(p => p.status === 'failed');
				if (!hasFailures) {
					const timer = setTimeout(() => {
						dismissed = true;
					}, 2000);
					return () => clearTimeout(timer);
				}
			}
		}
	});

	const visible = $derived(active || (progress.length > 0 && !dismissed));
	const allDone = $derived(progress.length > 0 && progress.every(p => p.status === 'success' || p.status === 'failed'));
	const succeededCount = $derived(progress.filter(p => p.status === 'success').length);
	const failedCount = $derived(progress.filter(p => p.status === 'failed').length);
</script>

{#if visible}
	<div
		class="fixed bottom-4 right-4 z-50 w-80"
		use:portalToBody
		in:fly|local={{ y: 20, duration: 200 }}
		out:fade|local={{ duration: 150 }}
	>
		<div class="rounded-lg border bg-card shadow-lg">
			<div class="flex items-center justify-between px-4 py-3 border-b">
				<h3 class="text-sm font-semibold">
					{#if allDone}
						Rescrape Complete
					{:else}
						Rescraping...
					{/if}
				</h3>
				{#if allDone}
					<button
						onclick={() => { dismissed = true; onDismiss?.(); }}
						class="text-muted-foreground hover:text-foreground transition-colors"
					>
						<X class="h-4 w-4" />
					</button>
				{/if}
			</div>
			<div class="max-h-64 overflow-y-auto px-4 py-2">
				{#each progress as item}
					<div class="flex items-center gap-2 py-1.5 text-sm">
						{#if item.status === 'success'}
							<Check class="h-4 w-4 text-green-500 shrink-0" />
						{:else if item.status === 'failed'}
							<X class="h-4 w-4 text-red-500 shrink-0" />
						{:else}
							<LoaderCircle class="h-4 w-4 animate-spin text-muted-foreground shrink-0" />
						{/if}
						<span class="truncate font-mono text-xs">{item.movie_id}</span>
						{#if item.error}
							<span class="text-xs text-red-500 truncate ml-auto" title={item.error}>failed</span>
						{/if}
					</div>
				{/each}
			</div>
			{#if progress.length > 0}
				<div class="px-4 py-2 border-t text-xs text-muted-foreground">
					{succeededCount} succeeded, {failedCount} failed
				</div>
			{/if}
		</div>
	</div>
{/if}
