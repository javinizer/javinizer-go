<script lang="ts">
	import type { FieldCategory } from '$lib/utils/completeness';
	import { fade } from 'svelte/transition';

	interface Props {
		breakdown: FieldCategory[];
		visible: boolean;
		id?: string;
	}

	let { breakdown, visible, id }: Props = $props();

	const essential = $derived(breakdown.filter(c => c.tier === 'essential'));
	const important = $derived(breakdown.filter(c => c.tier === 'important'));
	const niceToHave = $derived(breakdown.filter(c => c.tier === 'nice-to-have'));
</script>

<div
	role="tooltip"
	id={id}
	class="absolute bottom-full right-0 mb-2 bg-gray-900/95 text-white rounded-lg px-3 py-2 shadow-lg pointer-events-none z-20 whitespace-nowrap"
	class:invisible={!visible}
>
	{#if visible}
		<div transition:fade={{ duration: 150 }}>
			<div class="space-y-1.5">
				<div>
					<p class="text-[10px] font-semibold uppercase tracking-wider text-gray-400 mb-0.5">Essential</p>
					<div class="space-y-0.5">
						{#each essential as cat}
							<div class="flex items-center gap-1.5 text-xs">
								{#if cat.filled}
									<span class="text-green-400">✓</span>
								{:else}
									<span class="text-red-400/70">✗</span>
								{/if}
								<span>{cat.name}</span>
							</div>
						{/each}
					</div>
				</div>
				<div>
					<p class="text-[10px] font-semibold uppercase tracking-wider text-gray-400 mb-0.5">Important</p>
					<div class="space-y-0.5">
						{#each important as cat}
							<div class="flex items-center gap-1.5 text-xs">
								{#if cat.filled}
									<span class="text-green-400">✓</span>
								{:else}
									<span class="text-red-400/70">✗</span>
								{/if}
								<span>{cat.name}</span>
							</div>
						{/each}
					</div>
				</div>
				<div>
					<p class="text-[10px] font-semibold uppercase tracking-wider text-gray-400 mb-0.5">Nice-to-have</p>
					<div class="space-y-0.5">
						{#each niceToHave as cat}
							<div class="flex items-center gap-1.5 text-xs">
								{#if cat.filled}
									<span class="text-green-400">✓</span>
								{:else}
									<span class="text-red-400/70">✗</span>
								{/if}
								<span>{cat.name}</span>
							</div>
						{/each}
					</div>
				</div>
			</div>
		</div>
	{/if}
</div>
