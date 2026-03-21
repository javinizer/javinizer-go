<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { LoaderCircle, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import type { PosterCropBox, PosterCropMetrics } from '../review-utils';

	interface Props {
		show: boolean;
		posterCropSaving: boolean;
		posterCropLoadError: string | null;
		cropSourceURL: string;
		cropMetrics: PosterCropMetrics | null;
		cropBox: PosterCropBox | null;
		overlayStyle: string;
		onClose: () => void;
		onReset: () => void;
		onApply: () => void;
		onImageLoad: (event: Event) => void;
		onImageError: () => void;
		onCropMouseDown: (event: MouseEvent) => void;
	}

	let {
		show = $bindable(false),
		posterCropSaving,
		posterCropLoadError,
		cropSourceURL,
		cropMetrics,
		cropBox,
		overlayStyle,
		onClose,
		onReset,
		onApply,
		onImageLoad,
		onImageError,
		onCropMouseDown
	}: Props = $props();
</script>

{#if show}
	<div
		class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4"
		use:portalToBody
		in:fade|local={{ duration: 140 }}
		out:fade|local={{ duration: 120 }}
	>
		<div
			class="w-full max-w-5xl"
			in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }}
			out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}
		>
			<Card class="w-full flex flex-col max-h-[92vh]">
				<div class="p-4 border-b flex items-center justify-between">
					<div>
						<h2 class="text-lg font-semibold">Adjust Poster Crop</h2>
						<p class="text-xs text-muted-foreground">Drag the fixed crop box to choose the area to keep.</p>
					</div>
					<Button variant="ghost" size="icon" onclick={onClose} disabled={posterCropSaving}>
						{#snippet children()}
							<X class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>

				<div class="flex-1 min-h-0 overflow-hidden">
					<div class="relative w-full h-full p-10 bg-black/40 border-y min-h-[280px] flex items-center justify-center overflow-hidden">
						<img
							src={cropSourceURL}
							alt="Poster crop source"
							class="block max-w-full max-h-full select-none"
							draggable="false"
							onload={onImageLoad}
							onerror={onImageError}
						/>
						{#if cropMetrics && cropBox}
							<div
								class="absolute border-2 border-white cursor-move touch-none"
								style={overlayStyle}
								onmousedown={onCropMouseDown}
								aria-label="Poster crop selection"
								role="button"
								tabindex="-1"
							>
								<div class="absolute -bottom-7 right-0 bg-black/75 text-white text-[10px] px-1.5 py-0.5 rounded whitespace-nowrap">
									{cropBox.width} x {cropBox.height}
								</div>
							</div>
						{/if}
						{#if posterCropLoadError}
							<div class="absolute top-3 left-3 right-3 rounded border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive z-10">
								{posterCropLoadError}
							</div>
						{/if}
					</div>
				</div>

				<div class="p-4 border-t flex items-center justify-between gap-2">
					<Button variant="outline" onclick={onReset} disabled={!cropMetrics || posterCropSaving}>
						{#snippet children()}Reset Position{/snippet}
					</Button>
					<div class="flex items-center gap-2">
						<Button variant="outline" onclick={onClose} disabled={posterCropSaving}>
							{#snippet children()}Cancel{/snippet}
						</Button>
						<Button onclick={onApply} disabled={!cropBox || !!posterCropLoadError || posterCropSaving}>
							{#snippet children()}
								{#if posterCropSaving}
									<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
									Applying...
								{:else}
									Apply Crop
								{/if}
							{/snippet}
						</Button>
					</div>
				</div>
			</Card>
		</div>
	</div>
{/if}
