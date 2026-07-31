<script lang="ts">
	import Button from '$lib/components/ui/Button.svelte';
	import { LoaderCircle, Play, X } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		isUpdateMode: boolean;
		organizing: boolean;
		destinationPath: string;
		operationMode?: string;
		applyInvalid?: boolean;
		movieResultsLength: number;
		onCancel: () => void;
		onOrganizeAll: () => void;
	}

	let {
		isUpdateMode,
		organizing,
		destinationPath,
		operationMode = 'organize',
		applyInvalid = false,
		movieResultsLength,
		onCancel,
		onOrganizeAll
	}: Props = $props();
</script>

{#if !isUpdateMode}
	<div class="sticky bottom-0 z-30 mt-6">
		<div class="rounded-lg border border-border bg-background/95 backdrop-blur-sm px-4 py-3 shadow-lg">
			<div class="flex items-center justify-between gap-4">
				<p class="text-xs text-muted-foreground hidden sm:block">
					{m.review_action_bar_hint()}
				</p>
				<div class="flex items-center gap-3 ml-auto">
					<Button variant="outline" onclick={onCancel} disabled={organizing}>
						{#snippet children()}
							<X class="h-4 w-4 mr-2" />
							{m.common_cancel()}
						{/snippet}
					</Button>
					<Button onclick={onOrganizeAll} disabled={organizing || applyInvalid || (operationMode === 'organize' && !destinationPath.trim())}>
						{#snippet children()}
							{#if organizing}
								<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
							{:else}
								<Play class="h-4 w-4 mr-2" />
							{/if}
							{organizing ? m.review_organizing() : m.review_organize_files_button({ count: movieResultsLength })}
						{/snippet}
					</Button>
				</div>
			</div>
		</div>
	</div>
{/if}