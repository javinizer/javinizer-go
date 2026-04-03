<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fade, fly } from 'svelte/transition';
	import { ChevronDown } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		description?: string;
		isCollapsible?: boolean;
		isExpanded?: boolean;
		onToggle?: () => void;
		children: Snippet;
	}

	let {
		title,
		description,
		isCollapsible = false,
		isExpanded = true,
		onToggle,
		children
	}: Props = $props();
</script>

<div
	class="settings-subsection mt-6 first:mt-0"
	in:fly|local={{ y: 6, duration: 220, easing: cubicOut }}
	out:fade|local={{ duration: 140 }}
>
	<div class="subsection-header mb-4">
		{#if isCollapsible}
			<button
				type="button"
				class="flex items-center justify-between w-full text-left"
				onclick={onToggle}
			>
				<div>
					<h4 class="text-base font-semibold text-foreground">{title}</h4>
					{#if description}
						<p class="text-sm text-muted-foreground mt-1">{description}</p>
					{/if}
				</div>
				<ChevronDown class="h-4 w-4 shrink-0 ml-2 transition-transform {isExpanded ? 'rotate-180' : ''}" />
			</button>
		{:else}
			<h4 class="text-base font-semibold text-foreground">{title}</h4>
			{#if description}
				<p class="text-sm text-muted-foreground mt-1">{description}</p>
			{/if}
		{/if}
	</div>
	<div class="subsection-content space-y-0">
		{@render children()}
	</div>
</div>
