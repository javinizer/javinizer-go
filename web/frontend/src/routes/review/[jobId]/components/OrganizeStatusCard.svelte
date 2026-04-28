<script lang="ts">
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import { Check, CircleAlert, ChevronLeft } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';

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
		<h3 class="font-semibold mb-4">Organizing Files...</h3>

		<div class="mb-4">
			<div class="flex justify-between text-sm mb-1">
				<span>Progress</span>
				<span>{Math.round(organizeProgress)}%</span>
			</div>
			<div class="w-full bg-muted rounded-full h-2">
				<div
					class="bg-blue-600 h-2 rounded-full transition-all duration-300"
					style="width: {organizeProgress}%"
				></div>
			</div>
		</div>

		{#if fileStatuses.size > 0}
			<div class="space-y-2 max-h-64 overflow-y-auto">
				{#each Array.from(fileStatuses.entries()) as [filePath, status] (filePath)}
					<div
						animate:flip={{ duration: 220, easing: quintOut }}
						class="flex items-start gap-2 text-sm p-2 rounded {status.status === 'failed' ? 'bg-red-50' : 'bg-green-50'}"
					>
						{#if status.status === 'failed'}
							<CircleAlert class="h-4 w-4 text-red-600 shrink-0 mt-0.5" />
						{:else}
							<Check class="h-4 w-4 text-green-600 shrink-0 mt-0.5" />
						{/if}
						<div class="flex-1 min-w-0">
							<div class="font-medium truncate">{filePath.split(/[\\/]/).pop()}</div>
							{#if status.error}
								<div class="text-red-700 text-xs mt-1">{status.error}</div>
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
		<Card class="p-6 border-green-500 bg-green-50">
			<div class="flex items-start gap-3">
				<Check class="h-6 w-6 text-green-600 shrink-0" />
				<div class="flex-1">
					<h3 class="font-semibold mb-2 text-green-900">
						{isUpdateMode ? 'Update Complete!' : 'Organization Complete!'}
					</h3>
					<p class="text-sm text-green-800 mb-3">
						All {successCount} file(s) {isUpdateMode ? 'updated' : 'organized'} successfully
					</p>
					<p class="text-xs text-green-700">Redirecting to browse page in a few seconds...</p>
					<div class="mt-4">
						<Button onclick={onContinue} variant="outline">
							{#snippet children()}
								<ChevronLeft class="h-4 w-4 mr-2" />
								Return to Browse Now
							{/snippet}
						</Button>
					</div>
				</div>
			</div>
		</Card>
	{:else}
		<Card class="p-6 border-orange-500">
			<div class="flex items-start gap-3">
				<CircleAlert class="h-6 w-6 text-orange-600 shrink-0" />
				<div class="flex-1">
					<h3 class="font-semibold mb-2">Organization Completed with Errors</h3>
					<p class="text-sm text-muted-foreground mb-4">
						{successes.length} file(s) organized successfully, {failures.length} failed
					</p>

					<div class="space-y-2 max-h-96 overflow-y-auto">
						<h4 class="font-medium text-sm">Failed Files:</h4>
						{#each Array.from(fileStatuses.entries()).filter(([_, s]) => s.status === 'failed') as [filePath, status]}
							<div class="bg-red-50 p-3 rounded text-sm">
								<div class="font-medium">{filePath.split(/[\\/]/).pop()}</div>
								<div class="text-red-700 text-xs mt-1">{status.error}</div>
							</div>
						{/each}
					</div>

					<div class="mt-4 flex gap-2">
						<Button onclick={onRetryFailed}>
							{#snippet children()}
								Retry Failed
							{/snippet}
						</Button>
						<Button variant="outline" onclick={onContinue}>
							{#snippet children()}
								Continue Anyway
							{/snippet}
						</Button>
					</div>
				</div>
			</div>
		</Card>
	{/if}
{/if}
