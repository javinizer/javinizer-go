<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { Check, CircleAlert, ChevronLeft } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import * as m from '$lib/paraglide/messages';

	interface FileStatus {
		status: string;
		error?: string;
	}

	interface Props {
		organizeStatus: 'idle' | 'organizing' | 'completed' | 'failed';
		organizeProgress: number;
		fileStatuses: Map<string, FileStatus>;
		expectedOrganizeFilePaths: string[];
		isUpdateMode: boolean;
		onRetryFailed: () => void;
		onContinue: () => void;
	}

	let {
		organizeStatus,
		organizeProgress,
		fileStatuses,
		expectedOrganizeFilePaths,
		isUpdateMode,
		onRetryFailed,
		onContinue
	}: Props = $props();

	const failures = $derived(Array.from(fileStatuses.values()).filter((s) => s.status === 'failed'));
	const successes = $derived(Array.from(fileStatuses.values()).filter((s) => s.status === 'success'));
	const successCount = $derived(
		successes.length > 0 ? successes.length : expectedOrganizeFilePaths.length
	);
</script>

{#if organizeStatus === 'organizing'}
	<Card class="p-6">
		<h3 class="font-semibold mb-4">{m.review_organizing_files()}</h3>

		<div class="mb-4">
			<div class="flex justify-between text-sm mb-1">
				<span>{m.review_progress()}</span>
				<span>{Math.round(organizeProgress)}%</span>
			</div>
			<div class="w-full bg-muted rounded-full h-2">
				<div
					class="bg-blue-600 dark:bg-blue-500 h-2 rounded-full transition-all duration-300"
					style="width: {organizeProgress}%"
				></div>
			</div>
		</div>

		{#if fileStatuses.size > 0}
			<div class="space-y-2 max-h-64 overflow-y-auto">
				{#each Array.from(fileStatuses.entries()) as [filePath, status] (filePath)}
					<div
						animate:flip={{ duration: 220, easing: quintOut }}
						class="flex items-start gap-2 text-sm p-2 rounded {status.status === 'failed' ? 'bg-red-50 dark:bg-red-900/20' : 'bg-green-50 dark:bg-green-900/20'}"
					>
						{#if status.status === 'failed'}
							<CircleAlert class="h-4 w-4 text-red-600 dark:text-red-400 shrink-0 mt-0.5" />
						{:else}
							<Check class="h-4 w-4 text-green-600 dark:text-green-400 shrink-0 mt-0.5" />
						{/if}
						<div class="flex-1 min-w-0">
							<div class="font-medium truncate">{filePath.split(/[\\/]/).pop()}</div>
							{#if status.error}
								<div class="text-red-700 dark:text-red-400 text-xs mt-1">{status.error}</div>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</Card>
{/if}

{#if organizeStatus === 'completed'}
	{#if failures.length === 0}
		<Card class="p-6 border-green-500 dark:border-green-700 bg-green-50 dark:bg-green-900/20">
			<div class="flex items-start gap-3">
				<Check class="h-6 w-6 text-green-600 dark:text-green-400 shrink-0" />
				<div class="flex-1">
					<h3 class="font-semibold mb-2 text-green-900 dark:text-green-200">
						{isUpdateMode ? m.review_update_complete() : m.review_organization_complete()}
					</h3>
					<p class="text-sm text-green-800 dark:text-green-300 mb-3">
						{isUpdateMode ? m.review_all_files_updated({ count: successCount }) : m.review_all_files_organized({ count: successCount })}
					</p>
					<p class="text-xs text-green-700 dark:text-green-400">{m.review_redirecting_browse()}</p>
					<div class="mt-4">
						<Button onclick={onContinue} variant="outline">
							{#snippet children()}
								<ChevronLeft class="h-4 w-4 mr-2" />
								{m.review_return_to_browse_now()}
							{/snippet}
						</Button>
					</div>
				</div>
			</div>
		</Card>
	{:else}
		<Card class="p-6 border-orange-500 dark:border-orange-700">
			<div class="flex items-start gap-3">
				<CircleAlert class="h-6 w-6 text-orange-600 dark:text-orange-400 shrink-0" />
				<div class="flex-1">
					<h3 class="font-semibold mb-2">{m.review_org_completed_errors()}</h3>
					<p class="text-sm text-muted-foreground mb-4">
						{m.review_org_partial_summary({ successes: successes.length, failures: failures.length })}
					</p>

					<div class="space-y-2 max-h-96 overflow-y-auto">
						<h4 class="font-medium text-sm">{m.review_failed_files()}</h4>
						{#each Array.from(fileStatuses.entries()).filter(([_, s]) => s.status === 'failed') as [filePath, status]}
							<div class="bg-red-50 dark:bg-red-900/20 p-3 rounded text-sm">
								<div class="font-medium">{filePath.split(/[\\/]/).pop()}</div>
								<div class="text-red-700 dark:text-red-400 text-xs mt-1">{status.error}</div>
							</div>
						{/each}
					</div>

					<div class="mt-4 flex gap-2">
						<Button onclick={onRetryFailed}>
							{#snippet children()}{m.review_retry_failed()}
							{/snippet}
						</Button>
						<Button variant="outline" onclick={onContinue}>
							{#snippet children()}{m.review_continue_anyway()}
							{/snippet}
						</Button>
					</div>
				</div>
			</div>
		</Card>
	{/if}
{/if}
