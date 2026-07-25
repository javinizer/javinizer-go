<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { Image as ImageIcon, RotateCcw, Scissors, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import type { FileResult, Movie } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import ScreenshotManager from '$lib/components/ScreenshotManager.svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		show: boolean;
		currentMovie: Movie;
		currentResult: FileResult;
		displayPosterUrl?: string;
		showFieldScraperSources: boolean;
		canResetPoster: boolean;
		canResetCover: boolean;
		onUpdateCurrentMovie: (movie: Movie) => void;
		onUseScreenshotAsPoster: (url: string) => void;
		onUseScreenshotAsCover: (url: string) => void;
		onOpenPosterCropModal: () => void;
		onResetPoster: () => void;
		onResetCover: () => void;
	}

	let {
		show = $bindable(false),
		currentMovie,
		currentResult,
		displayPosterUrl,
		showFieldScraperSources,
		canResetPoster,
		canResetCover,
		onUpdateCurrentMovie,
		onUseScreenshotAsPoster,
		onUseScreenshotAsCover,
		onOpenPosterCropModal,
		onResetPoster,
		onResetCover
	}: Props = $props();

	function close() {
		show = false;
	}

	function handleKey(e: KeyboardEvent) {
		if (!show) return;
		if (e.key !== 'Escape') return;
		// If a nested dialog (cover/screenshot viewer, trailer, crop, or
		// confirmation) is open on top of this manager, let THAT dialog's own
		// Escape handler dismiss it — don't also close the manager underneath.
		// Checked synchronously at keydown time, while the nested dialog is
		// still mounted (its DOM is removed only after Svelte flushes).
		if (document.querySelectorAll('[role="dialog"][aria-modal="true"]').length > 1) return;
		close();
	}
</script>

<svelte:window onkeydown={handleKey} />

{#if show}
	<div
		class="fixed inset-0 bg-black/60 backdrop-blur-sm z-50 flex items-center justify-center p-4"
		use:portalToBody
		in:fade|local={{ duration: 140 }}
		out:fade|local={{ duration: 120 }}
		role="presentation"
		onclick={(e) => { if (e.target === e.currentTarget) close(); }}
	>
		<div
			class="w-full max-w-4xl"
			role="dialog"
			aria-modal="true"
			aria-labelledby="image-manager-modal-title"
			in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }}
			out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}
		>
			<Card class="w-full flex flex-col max-h-[90vh] overflow-hidden">
				<div class="px-6 py-4 border-b flex items-center justify-between gap-4">
					<div class="flex items-center gap-3 min-w-0">
						<div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
							<ImageIcon class="h-4 w-4" />
						</div>
						<div class="min-w-0">
							<h2 id="image-manager-modal-title" class="text-lg font-semibold tracking-tight truncate">
								{m.review_image_manager_title()}
							</h2>
							<p class="text-xs text-muted-foreground truncate">{currentMovie.id}</p>
						</div>
					</div>
					<Button variant="ghost" size="icon" onclick={close} aria-label={m.common_close()}>
						{#snippet children()}<X class="h-4 w-4" />{/snippet}
					</Button>
				</div>

				<div class="flex-1 overflow-y-auto p-6">
					<ScreenshotManager
						movie={currentMovie}
						{displayPosterUrl}
						onUpdate={onUpdateCurrentMovie}
						onUseScreenshotAsPoster={onUseScreenshotAsPoster}
						onUseScreenshotAsCover={onUseScreenshotAsCover}
						fieldSources={currentResult.field_sources}
						showFieldSources={showFieldScraperSources}
					/>
				</div>

				<div class="px-6 py-3 border-t flex items-center justify-between gap-3">
					<div class="flex items-center gap-2">
						<Button
							size="sm"
							variant="outline"
							onclick={onOpenPosterCropModal}
							disabled={!currentMovie.id}
							title={m.review_adjust_crop()}
						>
							{#snippet children()}<Scissors class="h-3.5 w-3.5 mr-1" />{m.review_adjust_crop()}{/snippet}
						</Button>
						{#if canResetPoster}
							<Button
								size="sm"
								variant="outline"
								onclick={onResetPoster}
								disabled={!currentMovie.id}
								title={m.review_reset_poster_title()}
							>
								{#snippet children()}<RotateCcw class="h-3.5 w-3.5 mr-1" />{m.review_reset_poster()}{/snippet}
							</Button>
						{/if}
						{#if canResetCover}
							<Button
								size="sm"
								variant="outline"
								onclick={onResetCover}
								disabled={!currentMovie.id}
								title={m.review_reset_cover_title()}
							>
								{#snippet children()}<RotateCcw class="h-3.5 w-3.5 mr-1" />{m.review_reset_cover()}{/snippet}
							</Button>
						{/if}
					</div>
					<Button variant="outline" onclick={close} class="ml-auto">
						{#snippet children()}{m.common_close()}{/snippet}
					</Button>
				</div>
			</Card>
		</div>
	</div>
{/if}