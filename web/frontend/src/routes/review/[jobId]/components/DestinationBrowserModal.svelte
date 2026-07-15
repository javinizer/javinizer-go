<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { portalToBody } from '$lib/actions/portal';
	import FileBrowser from '$lib/components/FileBrowser.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		show: boolean;
		destinationPath: string;
		tempDestinationPath: string;
		onCancel: () => void;
		onConfirm: () => void;
	}

	let {
		show = $bindable(false),
		destinationPath,
		tempDestinationPath = $bindable(''),
		onCancel,
		onConfirm
	}: Props = $props();

	function handleDestinationSelect(_files: string[]) {
		// Intentionally ignored: this browser is used for folder navigation only.
	}

	function handleDestinationPathChange(path: string) {
		tempDestinationPath = path;
	}
</script>

{#if show}
	<div
		class="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
		use:portalToBody
		in:fade|local={{ duration: 140 }}
		out:fade|local={{ duration: 120 }}
	>
		<div
			class="bg-background rounded-lg shadow-xl max-w-4xl w-full max-h-[80vh] flex flex-col"
			in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }}
			out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}
		>
			<div class="p-6 border-b flex items-center justify-between">
				<div>
					<h2 class="text-xl font-bold">{m.review_select_destination_folder()}</h2>
					<p class="text-sm text-muted-foreground mt-1">
						{m.common_select_folder_desc()}
					</p>
				</div>
				<button onclick={onCancel} class="text-muted-foreground hover:text-foreground transition-colors">
					✕
				</button>
			</div>

			<div class="flex-1 overflow-auto p-6">
				<FileBrowser
					initialPath={destinationPath || '/'}
					onFileSelect={handleDestinationSelect}
					onPathChange={handleDestinationPathChange}
					multiSelect={false}
					folderOnly={true}
				/>
			</div>

			<div class="p-6 border-t space-y-3">
				<div class="flex items-center gap-2">
					<span class="text-sm font-medium text-muted-foreground">{m.browse_selected_path()}</span>
					<code
						class="flex-1 px-3 py-1.5 bg-accent rounded text-sm font-mono text-foreground overflow-x-auto"
					>
						{tempDestinationPath || destinationPath || '/'}
					</code>
				</div>
				<div class="flex items-center justify-end gap-2">
					<Button variant="outline" onclick={onCancel}>
						{#snippet children()}{m.common_cancel()}{/snippet}
					</Button>
					<Button onclick={onConfirm}>
						{#snippet children()}{m.browse_use_this_folder()}{/snippet}
					</Button>
				</div>
			</div>
		</div>
	</div>
{/if}
