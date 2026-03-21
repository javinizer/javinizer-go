<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { X, CircleAlert, Check, ChevronRight, Info } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import type {
		NFOComparisonResponse,
		FieldDifference,
		MergeStatistics
	} from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import ProvenanceBadge from '$lib/components/ProvenanceBadge.svelte';

	interface Props {
		isOpen: boolean;
		comparison: NFOComparisonResponse | null;
		onClose: () => void;
		onApplyMerge?: () => void;
	}

	let { isOpen, comparison, onClose, onApplyMerge }: Props = $props();

	// Tab state
	let activeTab = $state<'differences' | 'stats' | 'raw'>('differences');

	// Format field names for display
	function formatFieldName(field: string): string {
		return field
			.split('_')
			.map((word) => word.charAt(0).toUpperCase() + word.slice(1))
			.join(' ');
	}

	// Format field value for display
	function formatValue(value: any): string {
		if (value === null || value === undefined) return 'Empty';
		if (typeof value === 'boolean') return value ? 'Yes' : 'No';
		if (typeof value === 'object') return JSON.stringify(value, null, 2);
		return String(value);
	}

	// Get stats summary text
	const statsSummary = $derived(
		comparison?.merge_stats
			? `${comparison.merge_stats.from_scraper} fields from scraper, ${comparison.merge_stats.from_nfo} from NFO, ${comparison.merge_stats.conflicts_resolved} conflicts resolved`
			: 'No merge statistics available'
	);
</script>

{#if isOpen && comparison}
	<!-- Modal overlay -->
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" use:portalToBody in:fade|local={{ duration: 140 }} out:fade|local={{ duration: 120 }}>
		<!-- Modal content -->
		<div class="relative max-h-[90vh] w-full max-w-5xl overflow-hidden rounded-lg bg-white dark:bg-gray-900 shadow-xl" in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }} out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}>
			<!-- Header -->
			<div class="flex items-center justify-between border-b border-gray-200 dark:border-gray-700 px-6 py-4">
				<div class="flex items-center gap-3">
					<h2 class="text-xl font-bold text-gray-900 dark:text-white">NFO Comparison</h2>
					{#if comparison.nfo_exists}
						<span class="rounded-full bg-green-100 dark:bg-green-900/20 px-3 py-1 text-xs font-medium text-green-800 dark:text-green-300">
							NFO Found
						</span>
					{:else}
						<span class="rounded-full bg-yellow-100 dark:bg-yellow-900/20 px-3 py-1 text-xs font-medium text-yellow-800 dark:text-yellow-300">
							No NFO
						</span>
					{/if}
				</div>
				<button
					onclick={onClose}
					class="rounded-full p-1 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
					aria-label="Close"
				>
					<X class="h-5 w-5" />
				</button>
			</div>

			<!-- Stats banner (if merge occurred) -->
			{#if comparison.merge_stats}
				<div class="bg-blue-50 dark:bg-blue-900/10 border-b border-blue-200 dark:border-blue-800 px-6 py-3">
					<div class="flex items-center gap-2 text-sm text-blue-900 dark:text-blue-300">
						<Info class="h-4 w-4" />
						<span class="font-medium">Merge Summary:</span>
						<span>{statsSummary}</span>
					</div>
				</div>
			{/if}

			<!-- Tabs -->
			<div class="border-b border-gray-200 dark:border-gray-700">
				<nav class="flex gap-4 px-6" aria-label="Tabs">
					<button
						onclick={() => (activeTab = 'differences')}
						class="border-b-2 px-1 py-3 text-sm font-medium transition-colors {activeTab ===
						'differences'
							? 'border-primary text-primary'
							: 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'}"
					>
						Differences
						{#if comparison.differences}
							<span class="ml-2 rounded-full bg-gray-200 dark:bg-gray-700 px-2 py-0.5 text-xs">
								{comparison.differences.length}
							</span>
						{/if}
					</button>
					<button
						onclick={() => (activeTab = 'stats')}
						class="border-b-2 px-1 py-3 text-sm font-medium transition-colors {activeTab ===
						'stats'
							? 'border-primary text-primary'
							: 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'}"
					>
						Statistics
					</button>
					<button
						onclick={() => (activeTab = 'raw')}
						class="border-b-2 px-1 py-3 text-sm font-medium transition-colors {activeTab ===
						'raw'
							? 'border-primary text-primary'
							: 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'}"
					>
						Raw Data
					</button>
				</nav>
			</div>

			<!-- Content -->
			<div class="overflow-y-auto p-6" style="max-height: calc(90vh - 200px);">
				{#if activeTab === 'differences'}
					{#if comparison.differences && comparison.differences.length > 0}
						<div class="space-y-4">
							{#each comparison.differences as diff (diff.field)}
								<div animate:flip={{ duration: 220, easing: quintOut }}>
									<Card>
									<div class="space-y-3">
										<!-- Field header -->
										<div class="flex items-center justify-between">
											<h3 class="font-medium text-gray-900 dark:text-white">
												{formatFieldName(diff.field)}
											</h3>
											{#if comparison.provenance?.[diff.field]}
												<ProvenanceBadge
													source={comparison.provenance[diff.field]}
													field={diff.field}
													showConfidence={true}
												/>
											{/if}
										</div>

										<!-- Value comparison -->
										<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
											<!-- NFO Value -->
											<div class="space-y-1">
												<div class="flex items-center gap-2 text-xs font-medium text-gray-500 dark:text-gray-400">
													<span>NFO</span>
												</div>
												<div class="rounded bg-green-50 dark:bg-green-900/10 border border-green-200 dark:border-green-800 p-3">
													<code class="text-sm text-gray-900 dark:text-gray-100 break-all">
														{formatValue(diff.nfo_value)}
													</code>
												</div>
											</div>

											<!-- Arrow -->
											<div class="flex items-center justify-center">
												<ChevronRight class="h-6 w-6 text-gray-400" />
											</div>

											<!-- Scraped Value -->
											<div class="space-y-1">
												<div class="flex items-center gap-2 text-xs font-medium text-gray-500 dark:text-gray-400">
													<span>Scraper</span>
												</div>
												<div class="rounded bg-blue-50 dark:bg-blue-900/10 border border-blue-200 dark:border-blue-800 p-3">
													<code class="text-sm text-gray-900 dark:text-gray-100 break-all">
														{formatValue(diff.scraped_value)}
													</code>
												</div>
											</div>
										</div>

										<!-- Merged value (if different from both) -->
										{#if diff.merged_value !== undefined && diff.merged_value !== diff.nfo_value && diff.merged_value !== diff.scraped_value}
											<div class="mt-3 pt-3 border-t border-gray-200 dark:border-gray-700">
												<div class="space-y-1">
													<div class="flex items-center gap-2 text-xs font-medium text-gray-500 dark:text-gray-400">
														<Check class="h-3 w-3" />
														<span>Merged Result</span>
													</div>
													<div class="rounded bg-purple-50 dark:bg-purple-900/10 border border-purple-200 dark:border-purple-800 p-3">
														<code class="text-sm text-gray-900 dark:text-gray-100 break-all">
															{formatValue(diff.merged_value)}
														</code>
													</div>
												</div>
											</div>
										{/if}

										<!-- Reason (if provided) -->
										{#if diff.reason}
											<div class="flex items-start gap-2 text-xs text-gray-600 dark:text-gray-400">
												<CircleAlert class="h-3 w-3 mt-0.5 shrink-0" />
												<span>{diff.reason}</span>
											</div>
										{/if}
									</div>
									</Card>
								</div>
							{/each}
						</div>
					{:else}
						<div class="text-center py-12">
							<Check class="h-12 w-12 mx-auto text-green-500 mb-4" />
							<p class="text-lg font-medium text-gray-900 dark:text-white mb-2">
								No Differences Found
							</p>
							<p class="text-sm text-gray-600 dark:text-gray-400">
								NFO data and scraped data are identical
							</p>
						</div>
					{/if}
				{:else if activeTab === 'stats'}
					{#if comparison.merge_stats}
						<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
							<Card>
								<div class="text-center">
									<div class="text-3xl font-bold text-blue-600 dark:text-blue-400">
										{comparison.merge_stats.total_fields}
									</div>
									<div class="text-sm text-gray-600 dark:text-gray-400 mt-1">Total Fields</div>
								</div>
							</Card>

							<Card>
								<div class="text-center">
									<div class="text-3xl font-bold text-green-600 dark:text-green-400">
										{comparison.merge_stats.from_scraper}
									</div>
									<div class="text-sm text-gray-600 dark:text-gray-400 mt-1">From Scraper</div>
								</div>
							</Card>

							<Card>
								<div class="text-center">
									<div class="text-3xl font-bold text-teal-600 dark:text-teal-400">
										{comparison.merge_stats.from_nfo}
									</div>
									<div class="text-sm text-gray-600 dark:text-gray-400 mt-1">From NFO</div>
								</div>
							</Card>

							<Card>
								<div class="text-center">
									<div class="text-3xl font-bold text-purple-600 dark:text-purple-400">
										{comparison.merge_stats.merged_arrays}
									</div>
									<div class="text-sm text-gray-600 dark:text-gray-400 mt-1">Merged Arrays</div>
								</div>
							</Card>

							<Card>
								<div class="text-center">
									<div class="text-3xl font-bold text-orange-600 dark:text-orange-400">
										{comparison.merge_stats.conflicts_resolved}
									</div>
									<div class="text-sm text-gray-600 dark:text-gray-400 mt-1">Conflicts Resolved</div>
								</div>
							</Card>

							<Card>
								<div class="text-center">
									<div class="text-3xl font-bold text-gray-600 dark:text-gray-400">
										{comparison.merge_stats.empty_fields}
									</div>
									<div class="text-sm text-gray-600 dark:text-gray-400 mt-1">Empty Fields</div>
								</div>
							</Card>
						</div>
					{:else}
						<div class="text-center py-12">
							<CircleAlert class="h-12 w-12 mx-auto text-gray-400 mb-4" />
							<p class="text-lg font-medium text-gray-900 dark:text-white mb-2">
								No Statistics Available
							</p>
							<p class="text-sm text-gray-600 dark:text-gray-400">
								Merge statistics will be available after merging
							</p>
						</div>
					{/if}
				{:else if activeTab === 'raw'}
					<div class="space-y-4">
						{#if comparison.nfo_data}
							<div>
								<h3 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">NFO Data</h3>
								<div class="rounded-lg bg-gray-50 dark:bg-gray-800 p-4 overflow-x-auto">
									<pre class="text-xs text-gray-900 dark:text-gray-100"><code>{JSON.stringify(
										comparison.nfo_data,
										null,
										2
									)}</code></pre>
								</div>
							</div>
						{/if}

						{#if comparison.scraped_data}
							<div>
								<h3 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
									Scraped Data
								</h3>
								<div class="rounded-lg bg-gray-50 dark:bg-gray-800 p-4 overflow-x-auto">
									<pre class="text-xs text-gray-900 dark:text-gray-100"><code>{JSON.stringify(
										comparison.scraped_data,
										null,
										2
									)}</code></pre>
								</div>
							</div>
						{/if}

						{#if comparison.merged_data}
							<div>
								<h3 class="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
									Merged Data
								</h3>
								<div class="rounded-lg bg-gray-50 dark:bg-gray-800 p-4 overflow-x-auto">
									<pre class="text-xs text-gray-900 dark:text-gray-100"><code>{JSON.stringify(
										comparison.merged_data,
										null,
										2
									)}</code></pre>
								</div>
							</div>
						{/if}
					</div>
				{/if}
			</div>

			<!-- Footer -->
			<div class="border-t border-gray-200 dark:border-gray-700 px-6 py-4 flex justify-between items-center">
				<div class="text-sm text-gray-600 dark:text-gray-400">
					{#if comparison.nfo_path}
						<span class="font-mono">{comparison.nfo_path}</span>
					{/if}
				</div>
				<div class="flex gap-3">
					<Button variant="secondary" onclick={onClose}>Close</Button>
					{#if onApplyMerge && comparison.merged_data}
						<Button variant="default" onclick={onApplyMerge}>Apply Merge</Button>
					{/if}
				</div>
			</div>
		</div>
	</div>
{/if}
