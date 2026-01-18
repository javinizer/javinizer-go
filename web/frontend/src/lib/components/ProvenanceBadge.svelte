<script lang="ts">
	import type { DataSource } from '$lib/api/types';
	import { Database, Globe } from 'lucide-svelte';

	interface Props {
		source: DataSource;
		field?: string;
		showConfidence?: boolean;
	}

	let { source, field, showConfidence = false }: Props = $props();

	// Determine badge color and icon based on source
	const sourceConfig = $derived({
		scraper: {
			color: 'bg-blue-100 text-blue-800 border-blue-200',
			darkColor: 'dark:bg-blue-900/20 dark:text-blue-300 dark:border-blue-800',
			icon: Globe,
			label: 'Scraper'
		},
		nfo: {
			color: 'bg-green-100 text-green-800 border-green-200',
			darkColor: 'dark:bg-green-900/20 dark:text-green-300 dark:border-green-800',
			icon: Database,
			label: 'NFO'
		}
	}[source.source] || {
		color: 'bg-gray-100 text-gray-800 border-gray-200',
		darkColor: 'dark:bg-gray-900/20 dark:text-gray-300 dark:border-gray-800',
		icon: Database,
		label: 'Unknown'
	});

	// Confidence color
	const confidenceColor = $derived(
		source.confidence >= 0.9
			? 'text-green-600 dark:text-green-400'
			: source.confidence >= 0.7
			? 'text-yellow-600 dark:text-yellow-400'
			: 'text-orange-600 dark:text-orange-400'
	);

	// Format last updated timestamp
	const lastUpdated = $derived(
		source.last_updated
			? new Date(source.last_updated).toLocaleDateString(undefined, {
					year: 'numeric',
					month: 'short',
					day: 'numeric'
			  })
			: null
	);

	// Get icon component
	const Icon = $derived(sourceConfig.icon);
</script>

<span
	class="inline-flex items-center gap-1 px-2 py-0.5 text-xs font-medium rounded border {sourceConfig.color} {sourceConfig.darkColor}"
	title={field
		? `${field}: ${sourceConfig.label}${showConfidence ? ` (${(source.confidence * 100).toFixed(0)}% confidence)` : ''}${lastUpdated ? ` - Updated ${lastUpdated}` : ''}`
		: `Source: ${sourceConfig.label}${showConfidence ? ` (${(source.confidence * 100).toFixed(0)}% confidence)` : ''}${lastUpdated ? ` - Updated ${lastUpdated}` : ''}`}
>
	<Icon class="h-3 w-3" />
	<span>{sourceConfig.label}</span>
	{#if showConfidence}
		<span class="ml-1 {confidenceColor}">
			{(source.confidence * 100).toFixed(0)}%
		</span>
	{/if}
</span>
