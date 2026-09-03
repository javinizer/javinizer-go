<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { FolderOutput, FolderPen, FilePenLine, ShieldCheck, FileText } from 'lucide-svelte';
	import type { VideoOperation } from '$lib/api/types';

	let { value = $bindable<VideoOperation | null>(), errorId, renameFile }: { value: VideoOperation | null; errorId?: string; renameFile?: boolean } = $props();
	let describedBy = $derived(errorId ? `video-operation-help ${errorId}` : 'video-operation-help');
	let radios: HTMLInputElement[] = $state([]);
	// #229: the rename-in-place description reflects the rename_file setting —
	// with rename_file=false only the (dedicated) folder is renamed.
	const options = $derived([
		{ value: 'organize' as const, label: m.browse_plan_organize(), description: m.browse_plan_organize_desc(), icon: FolderOutput },
		{ value: 'rename-in-place' as const, label: m.browse_plan_rename_in_place(), description: renameFile === false ? m.browse_plan_rename_in_place_desc_files_off() : m.browse_plan_rename_in_place_desc(), icon: FolderPen },
		{ value: 'rename-file' as const, label: m.browse_plan_rename_file(), description: m.browse_plan_rename_file_desc(), icon: FilePenLine },
		{ value: 'leave-in-place' as const, label: m.browse_plan_leave_in_place(), description: m.browse_plan_leave_in_place_desc(), icon: ShieldCheck },
		{ value: 'metadata-artwork' as const, label: m.browse_plan_metadata_artwork(), description: m.browse_plan_metadata_artwork_desc(), icon: FileText }
	]);

	function handleKeydown(event: KeyboardEvent, index: number) {
		let next = index;
		if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = (index + 1) % options.length;
		else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = (index - 1 + options.length) % options.length;
		else if (event.key === 'Home') next = 0;
		else if (event.key === 'End') next = options.length - 1;
		else return;
		event.preventDefault();
		value = options[next].value;
		queueMicrotask(() => radios[next]?.focus());
	}
</script>

<fieldset class="min-w-0 space-y-3" aria-describedby={describedBy}>
	<legend class="text-sm font-semibold">{m.browse_plan_video_operation()}</legend>
	<p id="video-operation-help" class="text-sm text-muted-foreground">{m.browse_plan_video_operation_desc()}</p>
	<div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
		{#each options as option, index}
			{@const selected = value === option.value}
			<label class="group relative flex min-h-28 cursor-pointer flex-col gap-2.5 rounded-lg border p-3.5 transition-colors focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-1 focus-within:ring-offset-background {selected ? 'border-primary-strong bg-primary-soft shadow-sm' : 'border-border bg-card hover:border-primary-soft hover:bg-accent-soft'}">
				<input bind:this={radios[index]} class="sr-only" type="radio" name="video-operation" value={option.value} bind:group={value} onkeydown={(event) => handleKeydown(event, index)} />
				<span class="flex items-center gap-2.5 pr-4">
					<span class="grid h-8 w-8 shrink-0 place-items-center rounded-md border transition-colors {selected ? 'border-primary bg-primary text-primary-foreground' : 'border-border bg-muted-soft text-muted-foreground group-hover:border-primary-soft group-hover:text-foreground'}" aria-hidden="true">
						<option.icon class="h-4 w-4" />
					</span>
					<span class="min-w-0 text-sm font-semibold leading-tight">{option.label}</span>
				</span>
				<span class="text-xs leading-relaxed text-muted-foreground">{option.description}</span>
				{#if selected}<span class="absolute right-3 top-3 grid h-5 w-5 place-items-center rounded-full bg-primary text-[0.625rem] font-bold leading-none text-primary-foreground" aria-hidden="true">✓</span>{/if}
			</label>
		{/each}
	</div>
</fieldset>

<style>
	label { min-height: 7rem; }
	@media (prefers-reduced-motion: reduce) {
		label, label span { transition: none; }
	}
</style>