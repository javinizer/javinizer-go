<script lang="ts">
	import { AlertTriangle, Search } from 'lucide-svelte';
	import type { FileResult } from '$lib/api/types';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		failedResults: FileResult[];
		onSearchManually: (result: FileResult) => void;
	}

	let { failedResults, onSearchManually }: Props = $props();

	function basename(path: string): string {
		return path.replace(/\\/g, '/').split('/').pop() ?? path;
	}
</script>

{#if failedResults.length > 0}
	<Card class="p-4">
		<div class="flex items-center gap-2 mb-3">
			<AlertTriangle class="h-4 w-4 text-warning shrink-0" />
			<p class="text-sm font-medium">
				{m.review_unidentified_files({ count: failedResults.length })}
			</p>
		</div>
		<div class="space-y-2">
			{#each failedResults as result (result.file_path)}
				<div class="flex items-start justify-between gap-3 rounded-md border px-3 py-2 text-sm">
					<div class="min-w-0 flex-1">
						<p class="font-mono truncate" title={result.file_path}>
							{basename(result.file_path)}
						</p>
						{#if result.error}
							<p class="text-xs text-destructive mt-0.5 truncate" title={result.error}>
								{result.error}
							</p>
						{/if}
					</div>
					<Button
						variant="outline"
						size="sm"
						onclick={() => onSearchManually(result)}
						class="shrink-0"
					>
						{#snippet children()}
							<Search class="h-3 w-3 mr-1" />
							{m.review_search_manually()}
						{/snippet}
					</Button>
				</div>
			{/each}
		</div>
	</Card>
{/if}
