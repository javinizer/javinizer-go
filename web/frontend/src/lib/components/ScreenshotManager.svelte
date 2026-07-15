<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { flip } from 'svelte/animate';
	import { quintOut } from 'svelte/easing';
	import type { Movie } from '$lib/api/types';
	import { apiClient } from '$lib/api/client';
	import { confirmDialog } from '$lib/stores/dialog.svelte';
	import Button from './ui/Button.svelte';
	import Card from './ui/Card.svelte';
	import ImageViewer from './ImageViewer.svelte';
import { tooltip } from '$lib/actions/tooltip';
	import VideoModal from './VideoModal.svelte';
	import { Plus, Trash2, Image as ImageIcon, ImagePlus, Play, RotateCcw, Info, ChevronDown } from 'lucide-svelte';

	interface Props {
		movie: Movie;
		displayPosterUrl?: string;
		onUpdate: (movie: Movie) => void;
		onUseScreenshotAsPoster?: (url: string) => void;
		onUseScreenshotAsCover?: (url: string) => void;
		fieldSources?: Record<string, string>;
		showFieldSources?: boolean;
	}

	let { movie, displayPosterUrl, onUpdate, onUseScreenshotAsPoster, onUseScreenshotAsCover, fieldSources, showFieldSources = false }: Props = $props();

	let screenshots = $state<string[]>([]);
	let posterUrl = $state('');
	let coverUrl = $state('');
	let trailerUrl = $state('');
	let newScreenshotUrl = $state('');

	// Reactive preview-error state — reset when the bound URL changes so a
	// corrected URL re-fetches instead of staying hidden by a stale onerror
	// (display:none) from a prior failed URL.
	let posterPreviewError = $state(false);
	let coverPreviewError = $state(false);
	let screenshotErrors = $state<Set<string>>(new Set());
	// The imgs below hide via the HTML `hidden` attribute (Tailwind preflight
	// [hidden]{display:none}). Do NOT add `block`/`flex` or other display utility
	// classes to those imgs — a display utility overrides [hidden] and breaks
	// error-hiding (the prior inline style.display='none' survived this).

	// Screenshot viewer modal state
	let showViewer = $state(false);
	let viewerIndex = $state(0);

	// Cover viewer modal state
	let showCoverViewer = $state(false);

	// Trailer modal state
	let showTrailerModal = $state(false);

	// Disclaimer expand state
	let showDisclaimer = $state(false);

	// Sync state when movie prop changes
	$effect(() => {
		screenshots = movie.screenshot_urls || [];
		posterUrl = movie.poster_url || '';
		coverUrl = movie.cover_url || '';
		trailerUrl = movie.trailer_url || '';
	});

	// Reset preview-error state when the bound URL/list changes, so a corrected
	// URL re-fetches instead of staying hidden by a stale onerror display:none.
	$effect(() => { posterUrl; displayPosterUrl; posterPreviewError = false; });
	$effect(() => { coverUrl; coverPreviewError = false; });
	// Track a derived signature of the screenshot URLs — not the array ref — so
	// the reset fires only on actual content change. An identical-content
	// reassignment (e.g. movie-sync handing back a new array with the same URLs)
	// leaves the signature unchanged, so this effect does not re-run and a
	// previously-errored img stays hidden (the browser does not re-fetch an
	// unchanged src, so un-hiding would flash a stale broken-image icon).
	let screenshotsSignature = $derived(screenshots.join('\u0000'));
	$effect(() => { screenshotsSignature; screenshotErrors = new Set(); });

	let clearCropState = $state(false);

	function notifyParent(clearCrop = false) {
		onUpdate({
			...movie,
			screenshot_urls: screenshots,
			poster_url: posterUrl,
			cover_url: coverUrl,
			trailer_url: trailerUrl,
			...((clearCrop || clearCropState) ? { should_crop_poster: false, cropped_poster_url: '' } : {})
		});
		clearCropState = false;
	}

	function onPosterUrlChange() {
		notifyParent(true);
	}

	function onFieldChange() {
		notifyParent();
	}

	function addScreenshot() {
		if (newScreenshotUrl.trim()) {
			screenshots = [...screenshots, newScreenshotUrl.trim()];
			newScreenshotUrl = '';
			notifyParent();
		}
	}

	function removeScreenshot(index: number) {
		screenshots = screenshots.filter((_, i) => i !== index);
		notifyParent();
	}

	async function removeAllScreenshots() {
		if (screenshots.length === 0) return;

		if (!(await confirmDialog(
			m.screenshot_remove_confirm_title(),
			m.screenshot_remove_confirm_body({ count: screenshots.length }),
			{ variant: 'danger', confirmLabel: m.screenshot_remove_confirm_action() }
		))) return;

		screenshots = [];
		showViewer = false;
		viewerIndex = 0;
		notifyParent();
	}

	async function useScreenshotAsPoster(url: string) {
		if (onUseScreenshotAsPoster) {
			onUseScreenshotAsPoster(url);
			return;
		}

		if (!(await confirmDialog(m.screenshot_use_poster_title(), m.screenshot_use_poster_body(), { confirmLabel: m.screenshot_use_poster_action() }))) return;

		clearCropState = true;
		posterUrl = url;
		notifyParent();
	}

	async function useScreenshotAsCover(url: string) {
		if (onUseScreenshotAsCover) {
			onUseScreenshotAsCover(url);
			return;
		}

		if (!(await confirmDialog(m.screenshot_use_cover_title(), m.screenshot_use_cover_body(), { confirmLabel: m.screenshot_use_cover_action() }))) return;

		coverUrl = url;
		notifyParent();
	}

	function handleKeyPress(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			addScreenshot();
		}
	}

	// Screenshot viewer functions
	function openViewer(index: number) {
		viewerIndex = index;
		showViewer = true;
	}

	function closeViewer() {
		showViewer = false;
	}

	function previewImageURL(url: string): string {
		if (!url) return '';
		if (url.startsWith('/api/v1/')) return url;
		if (url.startsWith('/')) return url;
		return apiClient.getPreviewImageURL(url);
	}

	function sourceText(fieldKey: string): string | null {
		if (!showFieldSources || !fieldSources) return null;
		const rawSource = fieldSources[fieldKey];
		if (!rawSource) return null;

		const source = rawSource.trim();
		if (!source) return null;

		const normalized = source.toLowerCase();
		if (normalized === 'nfo') return m.screenshot_via_nfo();
		if (normalized === 'merged') return m.screenshot_via_merged();
		if (normalized === 'empty') return m.screenshot_empty_source();
		return m.screenshot_via_source({ source });
	}
</script>

<div class="space-y-6">
	<!-- Poster Image -->
	<div>
		<h3 class="text-lg font-semibold mb-3 flex items-center gap-2">
			<span>{m.screenshot_poster_image()}</span>
			{#if sourceText('poster_url')}
				<span class="text-xs font-normal text-muted-foreground">{sourceText('poster_url')}</span>
			{/if}
		</h3>
		<div class="space-y-3">
			<div>
				<label for="poster-url" class="text-sm font-medium mb-1 block">
					{m.screenshot_poster_url()}
					{#if sourceText('poster_url')}
						<span class="text-xs font-normal text-muted-foreground ml-2">{sourceText('poster_url')}</span>
					{/if}
				</label>
				<input
					id="poster-url"
					type="url"
					bind:value={posterUrl}
					onchange={onPosterUrlChange}
					placeholder="https://..."
					class="w-full px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
				/>
			</div>
			<div>
				<div class="text-sm font-medium mb-1 block">
					{m.screenshot_preview()}{movie.should_crop_poster ? ' (' + m.screenshot_preview_cropped().replace('Preview (Cropped)', 'Cropped') + ')' : ''}
				</div>
				{#if displayPosterUrl || posterUrl}
					<div class="w-full max-w-xs aspect-2/3 overflow-hidden rounded border relative">
						{#if movie.should_crop_poster && !displayPosterUrl}
							<!-- Crop to show only right 47.2% of image (removes promotional text on left) -->
							<!-- Only apply cropping if displayPosterUrl is not available (displayPosterUrl is already cropped if temp_poster_url) -->
							<img
								src={posterUrl}
								alt={m.screenshot_poster_alt()}
								class="absolute h-full"
								style="right: 0; width: auto; min-width: 211.8%; object-fit: cover; object-position: right center;" hidden={posterPreviewError}
								onerror={() => { posterPreviewError = true; }}
							/>
						{:else}
							<!-- Use displayPosterUrl (temp_poster_url if available) or posterUrl directly without cropping -->
							<img
								src={displayPosterUrl || posterUrl}
								alt={m.screenshot_poster_alt()}
								class="w-full h-full object-contain" hidden={posterPreviewError}
								onerror={() => { posterPreviewError = true; }}
							/>
						{/if}
					</div>
				{:else}
					<div
						class="w-full max-w-xs aspect-2/3 bg-accent rounded border flex items-center justify-center text-muted-foreground"
					>
						<div class="text-center">
							<ImageIcon class="h-12 w-12 mx-auto mb-2 opacity-50" />
							<p class="text-sm">{m.screenshot_no_poster()}</p>
						</div>
					</div>
				{/if}
			</div>
		</div>
	</div>

	<!-- Cover/Fanart Image -->
	<div>
		<h3 class="text-lg font-semibold mb-3 flex items-center gap-2">
			<span>{m.screenshot_cover_image()}</span>
			{#if sourceText('cover_url')}
				<span class="text-xs font-normal text-muted-foreground">{sourceText('cover_url')}</span>
			{/if}
		</h3>
		<div class="space-y-3">
			<div>
				<label for="cover-url" class="text-sm font-medium mb-1 block">
					{m.screenshot_cover_url()}
					{#if sourceText('cover_url')}
						<span class="text-xs font-normal text-muted-foreground ml-2">{sourceText('cover_url')}</span>
					{/if}
				</label>
				<input
					id="cover-url"
					type="url"
					bind:value={coverUrl}
					onchange={onFieldChange}
					placeholder="https://..."
					class="w-full px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
				/>
			</div>
			<div>
				<div class="text-sm font-medium mb-1 block">{m.screenshot_preview()}</div>
				{#if coverUrl}
					<button
						onclick={() => (showCoverViewer = true)}
						class="w-full rounded border overflow-hidden hover:opacity-80 transition-opacity cursor-pointer"
					>
							<img
								src={previewImageURL(coverUrl)}
								alt={m.screenshot_cover_alt()}
								class="w-full" hidden={coverPreviewError}
							onerror={() => { coverPreviewError = true; }}
						/>
					</button>
				{:else}
					<div
						class="w-full h-48 bg-accent rounded border flex items-center justify-center text-muted-foreground"
					>
						<div class="text-center">
							<ImageIcon class="h-12 w-12 mx-auto mb-2 opacity-50" />
							<p class="text-sm">{m.screenshot_no_cover()}</p>
						</div>
					</div>
				{/if}
			</div>
		</div>
	</div>

	<!-- Trailer -->
	<div>
		<h3 class="text-lg font-semibold mb-3 flex items-center gap-2">
			<span>{m.screenshot_trailer()}</span>
			{#if sourceText('trailer_url')}
				<span class="text-xs font-normal text-muted-foreground">{sourceText('trailer_url')}</span>
			{/if}
		</h3>
		<div class="space-y-3">
			<div>
				<label for="trailer-url" class="text-sm font-medium mb-1 block">
					{m.screenshot_trailer_url()}
					{#if sourceText('trailer_url')}
						<span class="text-xs font-normal text-muted-foreground ml-2">{sourceText('trailer_url')}</span>
					{/if}
				</label>
				<input
					id="trailer-url"
					type="url"
					bind:value={trailerUrl}
					onchange={onFieldChange}
					placeholder="https://..."
					class="w-full px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
				/>
			</div>
			<div>
				<div class="text-sm font-medium mb-1 block">{m.screenshot_preview()}</div>
				{#if trailerUrl}
					<button
						onclick={() => (showTrailerModal = true)}
						class="w-full h-48 bg-accent rounded border flex items-center justify-center text-primary hover:bg-accent/80 transition-colors cursor-pointer"
					>
						<div class="text-center">
							<Play class="h-12 w-12 mx-auto mb-2" />
							<p class="text-sm font-medium">{m.screenshot_play_trailer()}</p>
						</div>
					</button>
				{:else}
					<div
						class="w-full h-48 bg-accent rounded border flex items-center justify-center text-muted-foreground"
					>
						<div class="text-center">
							<Play class="h-12 w-12 mx-auto mb-2 opacity-50" />
							<p class="text-sm">{m.screenshot_no_trailer()}</p>
						</div>
					</div>
				{/if}
			</div>
		</div>
	</div>

	<!-- Screenshots -->
	<div>
		<div class="flex items-center justify-between mb-3">
			<h3 class="text-lg font-semibold flex items-center gap-2">
				<span>{m.screenshot_section_title({ count: screenshots.length })}</span>
				{#if sourceText('screenshot_urls')}
					<span class="text-xs font-normal text-muted-foreground">{sourceText('screenshot_urls')}</span>
				{/if}
			</h3>
			{#if screenshots.length > 0}
				<Button
					variant="outline"
					size="sm"
					onclick={removeAllScreenshots}
					class="text-destructive border-destructive/30 hover:bg-destructive/10 hover:text-destructive"
					title={m.screenshot_remove_all_title()}
				>
					{#snippet children()}
						<Trash2 class="h-4 w-4" />
						{m.screenshot_remove_all()}
					{/snippet}
				</Button>
			{/if}
		</div>

		<!-- Placeholder filtering disclaimer (collapsible) -->
		<div class="mb-3">
			<button
				onclick={() => (showDisclaimer = !showDisclaimer)}
				class="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors"
			>
				<Info class="h-3 w-3" />
				<span>{m.screenshot_why_fewer()}</span>
				<ChevronDown class="h-3 w-3 transition-transform {showDisclaimer ? 'rotate-180' : ''}" />
			</button>
			{#if showDisclaimer}
				<p class="mt-1.5 text-xs text-muted-foreground pl-5">
					{m.screenshot_filter_disclaimer()}
				</p>
			{/if}
		</div>

		<!-- Add Screenshot Form -->
		<div class="flex gap-2 mb-4">
			<input
				type="url"
				bind:value={newScreenshotUrl}
				onkeydown={handleKeyPress}
				placeholder={m.screenshot_add_placeholder()}
				class="flex-1 px-3 py-2 border rounded-md bg-background focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm"
			/>
			<Button onclick={addScreenshot} disabled={!newScreenshotUrl.trim()}>
				{#snippet children()}
					<Plus class="h-4 w-4 mr-2" />
					{m.common_add()}
				{/snippet}
			</Button>
		</div>

		<!-- Screenshots Grid -->
		{#if screenshots.length === 0}
			<div class="text-center py-8 text-muted-foreground border-2 border-dashed rounded-lg">
				<ImageIcon class="h-12 w-12 mx-auto mb-2 opacity-50" />
				<p>{m.screenshot_empty()}</p>
				<p class="text-xs mt-1">{m.screenshot_empty_hint()}</p>
			</div>
		{:else}
			<div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
				{#each screenshots as url, index (`${url}-${index}`)}
					<div animate:flip={{ duration: 220, easing: quintOut }}>
						<Card class="p-2 group relative">
						<button
							onclick={() => openViewer(index)}
							class="w-full cursor-pointer hover:opacity-80 transition-opacity"
						>
							<img
								src={previewImageURL(url)}
								alt={m.screenshot_alt_index({ index: index + 1 })}
								class="w-full aspect-video object-cover rounded" hidden={screenshotErrors.has(url)}
								onerror={() => { screenshotErrors = new Set([...screenshotErrors, url]); }}
							/>
						</button>
						<div class="mt-2 flex items-center gap-1">
							<p class="text-xs text-muted-foreground truncate flex-1" title={url}>
								{m.screenshot_alt_index({ index: index + 1 })}
							</p>
							<span class="inline-flex" use:tooltip={m.screenshot_use_as_poster()}>
								<Button
										variant="ghost"
										size="sm"
										onclick={() => useScreenshotAsPoster(url)}
										class="p-1 h-auto"
								>
										{#snippet children()}
												<ImagePlus class="h-3 w-3" />
										{/snippet}
								</Button>
							</span>
							<span class="inline-flex" use:tooltip={m.screenshot_use_as_cover()}>
								<Button
										variant="ghost"
										size="sm"
										onclick={() => useScreenshotAsCover(url)}
										class="p-1 h-auto"
								>
										{#snippet children()}
												<ImageIcon class="h-3 w-3" />
										{/snippet}
								</Button>
							</span>
							<Button
								variant="ghost"
								size="sm"
								onclick={() => removeScreenshot(index)}
								class="text-destructive hover:bg-destructive/10 p-1 h-auto"
							>
								{#snippet children()}
									<Trash2 class="h-3 w-3" />
								{/snippet}
							</Button>
						</div>
						</Card>
					</div>
				{/each}
			</div>
		{/if}
	</div>
</div>

<!-- Screenshot Viewer Modal -->
<ImageViewer
	bind:show={showViewer}
	images={screenshots.map((url) => previewImageURL(url))}
	initialIndex={viewerIndex}
	onClose={closeViewer}
/>

<!-- Cover Viewer Modal -->
<ImageViewer
	bind:show={showCoverViewer}
	images={[previewImageURL(coverUrl)]}
	initialIndex={0}
	title={m.screenshot_cover_viewer_title()}
	onClose={() => (showCoverViewer = false)}
/>

<!-- Trailer Modal -->
<VideoModal
	bind:show={showTrailerModal}
	videoUrl={trailerUrl}
	title={m.screenshot_trailer_viewer_title()}
	onClose={() => (showTrailerModal = false)}
/>
