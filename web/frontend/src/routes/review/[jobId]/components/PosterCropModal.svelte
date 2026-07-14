<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { LoaderCircle, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import { computeCropPreview, type PosterCropBox, type PosterCropMetrics } from '../review-utils';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		show: boolean;
		posterCropSaving: boolean;
		posterCropLoadError: string | null;
		cropSourceURL: string;
		cropMetrics: PosterCropMetrics | null;
		cropBox: PosterCropBox | null;
		overlayStyle: string;
		configMaxPosterHeight: number;
		maxPosterHeight: number | null;
		onClose: () => void;
		onReset: () => void;
		onApply: () => void;
		onImageLoad: (event: Event) => void;
		onImageError: () => void;
		onCropMouseDown: (event: MouseEvent) => void;
		onMaxPosterHeightChange: (value: number | null) => void;
	}

	let {
		show = $bindable(false),
		posterCropSaving,
		posterCropLoadError,
		cropSourceURL,
		cropMetrics,
		cropBox,
		overlayStyle,
		configMaxPosterHeight,
		maxPosterHeight,
		onClose,
		onReset,
		onApply,
		onImageLoad,
		onImageError,
		onCropMouseDown,
		onMaxPosterHeightChange
	}: Props = $props();

	// Input field shows the config value when maxPosterHeight is null
	// (meaning "use config default"). User-typed values override it.
	let maxPosterHeightInput = $derived(maxPosterHeight ?? configMaxPosterHeight);

	let preview = $derived(computeCropPreview(cropBox, maxPosterHeightInput));

	function handleMaxHeightInput(event: Event) {
		const target = event.target as HTMLInputElement;
		const trimmed = target.value.trim();
		if (trimmed === '') {
			onMaxPosterHeightChange(null);
			return;
		}
		const parsed = Number.parseInt(trimmed, 10);
		if (Number.isNaN(parsed) || parsed < 0) return;
		onMaxPosterHeightChange(parsed);
	}

	function resetToConfigDefault() {
		onMaxPosterHeightChange(null);
	}
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
						<h2 class="text-lg font-semibold">{m.review_adjust_poster_crop()}</h2>
						<p class="text-xs text-muted-foreground">{m.review_poster_crop_drag_hint()}</p>
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
							alt={m.review_poster_crop_source_alt()}
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
								aria-label={m.review_poster_crop_selection_aria()}
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

				<div class="p-4 border-t flex flex-col gap-3">
					<div class="flex items-center justify-between gap-4 text-xs">
						<!-- Live output preview -->
						<div class="flex items-center gap-3 text-muted-foreground">
							<span>{m.review_output_label()}</span>
							<code class="px-1.5 py-0.5 rounded bg-muted text-foreground">
								{preview.outputWidth}×{preview.outputHeight}px
							</code>
							{#if preview.ratioLabel}
								<span class="text-muted-foreground">({preview.ratioLabel})</span>
							{/if}
							{#if preview.willResize}
								<span class="text-amber-500 dark:text-amber-400 font-medium">
									{m.review_downscaled_from({ width: cropBox?.width ?? 0, height: cropBox?.height ?? 0 })}
								</span>
							{:else if cropBox}
								<span class="text-emerald-500 dark:text-emerald-400 font-medium">
									{m.review_source_resolution_preserved()}
								</span>
							{/if}
						</div>

						<!-- Max poster height input -->
						<div class="flex items-center gap-2">
							<label for="max-poster-height" class="text-muted-foreground whitespace-nowrap">
								{m.review_max_poster_height()}
							</label>
							<input
								id="max-poster-height"
								type="number"
								min="0"
								class="w-24 px-2 py-1 text-xs rounded border border-input bg-background"
								value={maxPosterHeightInput}
								oninput={handleMaxHeightInput}
							/>
							<span class="text-muted-foreground">
								{configMaxPosterHeight !== 0 ? m.review_max_poster_height_hint_with_config({ configValue: configMaxPosterHeight }) : m.review_max_poster_height_hint_no_cap()}
							</span>
							{#if maxPosterHeight !== null}
								<button
									type="button"
									class="text-xs text-primary hover:underline"
									onclick={resetToConfigDefault}
								>
									{m.review_reset_to_config()}
								</button>
							{/if}
						</div>
					</div>

					<div class="flex items-center justify-between gap-2">
						<Button variant="outline" onclick={onReset} disabled={!cropMetrics || posterCropSaving}>
							{#snippet children()}{m.review_reset_position()}{/snippet}
						</Button>
						<div class="flex items-center gap-2">
							<Button variant="outline" onclick={onClose} disabled={posterCropSaving}>
								{#snippet children()}{m.common_cancel()}{/snippet}
							</Button>
							<Button onclick={onApply} disabled={!cropBox || !!posterCropLoadError || posterCropSaving}>
								{#snippet children()}
									{#if posterCropSaving}
										<LoaderCircle class="h-4 w-4 mr-2 animate-spin" />
										{m.review_applying()}
									{:else}
										{m.review_apply_crop()}
									{/if}
								{/snippet}
							</Button>
						</div>
					</div>
				</div>
			</Card>
		</div>
	</div>
{/if}
