<script lang="ts">
	import { ChevronDown, ChevronRight } from 'lucide-svelte';
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		description?: string;
		defaultExpanded?: boolean;
		children: Snippet;
	}

	let {
		title,
		description,
		defaultExpanded = false,
		children
	}: Props = $props();

	let expanded = $state(false);
	let initialized = $state(false);

	// Initialize expanded state from prop only once
	$effect(() => {
		if (!initialized) {
			expanded = defaultExpanded;
			initialized = true;
		}
	});

	function toggle() {
		expanded = !expanded;
	}
</script>

<section class="settings-section mb-6 border border-border rounded-lg bg-card overflow-hidden">
	<button
		type="button"
		class="section-header w-full flex items-start gap-3 p-4 hover:bg-accent/50 transition-colors cursor-pointer text-left {expanded ? 'border-b border-border' : ''}"
		onclick={toggle}
		aria-expanded={expanded}
	>
		<div class="chevron mt-0.5 text-muted-foreground">
			{#if expanded}
				<ChevronDown class="h-5 w-5" />
			{:else}
				<ChevronRight class="h-5 w-5" />
			{/if}
		</div>
		<div class="header-content flex-1">
			<h3 class="text-lg font-semibold text-foreground">{title}</h3>
			{#if description}
				<p class="text-sm text-muted-foreground mt-1">{description}</p>
			{/if}
		</div>
	</button>

	{#if expanded}
		<div class="section-content p-4">
			{@render children()}
		</div>
	{/if}
</section>
