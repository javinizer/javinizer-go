<script lang="ts">
	import { fade } from 'svelte/transition';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import ActressEditor from '$lib/components/ActressEditor.svelte';
	import ImageViewer from '$lib/components/ImageViewer.svelte';
	import VideoModal from '$lib/components/VideoModal.svelte';
	import DestinationBrowserModal from './components/DestinationBrowserModal.svelte';
	import DestinationSettingsCard from './components/DestinationSettingsCard.svelte';
	import ImagesMediaCard from './components/ImagesMediaCard.svelte';
	import MovieNavigationCard from './components/MovieNavigationCard.svelte';
	import MovieMetadataCard from './components/MovieMetadataCard.svelte';
	import OrganizeStatusCard from './components/OrganizeStatusCard.svelte';
	import PosterCropModal from './components/PosterCropModal.svelte';
	import ReviewActionBar from './components/ReviewActionBar.svelte';
	import ReviewGridCard from './components/ReviewGridCard.svelte';
	import ReviewHeader from './components/ReviewHeader.svelte';
	import ReviewMediaSidebar from './components/ReviewMediaSidebar.svelte';
	import RescrapeModal from './components/RescrapeModal.svelte';
	import BulkRescrapeProgress from './components/BulkRescrapeProgress.svelte';
	import SourceFilesCard from './components/SourceFilesCard.svelte';
	import UnidentifiedFilesCard from './components/UnidentifiedFilesCard.svelte';
	import { createReviewState } from './stores/review-state.svelte';
	import { shouldSyncTab, buildTabUrl, type ReviewTabId } from '$lib/utils/review-tab-sync';
	import {
		AlertTriangle,
		ChevronLeft,
		CircleAlert,
		Film
	} from 'lucide-svelte';

	const s = createReviewState($page);

	const initialTabParam = $page.url.searchParams.get('tab');
	let activeTab = $state<ReviewTabId>(initialTabParam === 'failed' ? 'failed' : 'movies');

	const hasMovies = $derived(s.movieResults.length > 0);
	const hasFailed = $derived(s.failedResults.length > 0);

	$effect(() => {
		if (!hasMovies && hasFailed) {
			activeTab = 'failed';
		} else if (hasMovies && activeTab === 'failed' && !hasFailed) {
			activeTab = 'movies';
		}
	});

	$effect(() => {
		const currentParam = $page.url.searchParams.get('tab');
		if (!shouldSyncTab(currentParam, activeTab)) return;
		void goto(buildTabUrl($page.url, activeTab), { replaceState: true, noScroll: true, keepFocus: true });
	});
</script>

<div class="container mx-auto px-4 py-8">
	<div class="max-w-7xl mx-auto space-y-6">
		{#if s.loading}
			<div class="text-center py-12">
				<p class="text-muted-foreground">Loading batch job...</p>
			</div>
		{:else if s.error}
			<Card class="p-6">
				<div class="text-center text-destructive">
					<CircleAlert class="h-12 w-12 mx-auto mb-4" />
					<p class="font-semibold">Error</p>
					<p class="text-sm">{s.error}</p>
					<Button onclick={() => goto('/browse')} class="mt-4">
						{#snippet children()}
							<ChevronLeft class="h-4 w-4 mr-2" />
							Back to Browse
						{/snippet}
					</Button>
				</div>
			</Card>
		{:else if s.job && !hasMovies && !hasFailed}
			<Card class="p-6">
				<div class="text-center">
					<p class="text-muted-foreground">No movies to review</p>
					<Button onclick={() => goto('/browse')} class="mt-4">
						{#snippet children()}
							<ChevronLeft class="h-4 w-4 mr-2" />
							Back to Browse
						{/snippet}
					</Button>
				</div>
			</Card>
		{:else if s.job}
			<div class="border-b border-border">
				<nav class="flex gap-1" aria-label="Review tabs">
					<button
						type="button"
						role="tab"
						id="movies-tab"
						aria-selected={activeTab === 'movies'}
						aria-controls="movies-panel"
						onclick={() => (activeTab = 'movies')}
						disabled={!hasMovies}
						class="inline-flex items-center gap-2 border-b-2 px-4 py-3 text-sm font-medium transition-colors
							{activeTab === 'movies'
								? 'border-primary text-primary'
								: 'border-transparent text-muted-foreground hover:border-border hover:text-foreground'}
							{!hasMovies ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}"
					>
						<Film class="h-4 w-4" />
						Movies
						{#if hasMovies}
							<span class="rounded-full bg-muted px-2 py-0.5 text-xs font-normal">
								{s.movieResults.length}
							</span>
						{/if}
					</button>
					<button
						type="button"
						role="tab"
						id="failed-tab"
						aria-selected={activeTab === 'failed'}
						aria-controls="failed-panel"
						onclick={() => (activeTab = 'failed')}
						disabled={!hasFailed}
						class="inline-flex items-center gap-2 border-b-2 px-4 py-3 text-sm font-medium transition-colors
							{activeTab === 'failed'
								? 'border-primary text-primary'
								: 'border-transparent text-muted-foreground hover:border-border hover:text-foreground'}
							{!hasFailed ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'}"
					>
						<AlertTriangle class="h-4 w-4" />
						Failed
						{#if hasFailed}
							<span class="rounded-full bg-destructive/10 text-destructive px-2 py-0.5 text-xs font-normal">
								{s.failedResults.length}
							</span>
						{/if}
					</button>
				</nav>
			</div>

			<div
				role="tabpanel"
				id="movies-panel"
				aria-labelledby="movies-tab"
				hidden={activeTab !== 'movies'}
				class={activeTab === 'movies' ? 'space-y-6' : ''}
			>
				{#if activeTab === 'movies' && s.currentMovie && s.currentResult}
					<ReviewHeader
						isUpdateMode={s.isUpdateMode}
						canOrganize={s.canOrganize}
						organizing={s.organizing}
						movieResultsLength={s.movieResults.length}
						destinationPath={s.destinationPath}
						bind:viewMode={s.viewMode}
						bind:forceOverwrite={s.forceOverwrite}
						bind:preserveNfo={s.preserveNfo}
						bind:skipNfo={s.skipNfo}
						bind:skipDownload={s.skipDownload}
						selectedCount={s.selectedCount}
						allSelected={s.allSelected}
						bulkExcluding={s.bulkExcludeMutation.isPending}
						bulkRescraping={s.bulkRescraping}
						completenessFilter={s.completenessFilter}
						tierCounts={s.tierCounts}
						selectionMode={s.selectionMode}
						onToggleCompletenessTier={s.toggleCompletenessTier}
						onToggleSelectionMode={s.toggleSelectionMode}
						onSelectAll={s.selectAllMovies}
						onDeselectAll={s.deselectAllMovies}
						onBulkExclude={s.bulkExcludeMovies}
						onBulkRescrape={s.openBulkRescrapeModal}
						onClose={() => goto('/browse')}
						onUpdateAll={s.updateAll}
						onOrganizeAll={s.organizeAll}
					/>

					<OrganizeStatusCard
						organizeStatus={s.organizeStatus}
						organizeProgress={s.organizeProgress}
						fileStatuses={s.fileStatuses}
						expectedOrganizeFilePaths={s.expectedOrganizeFilePaths}
						isUpdateMode={s.isUpdateMode}
						onRetryFailed={s.retryFailed}
						onContinue={() => goto('/browse')}
					/>

					{#if s.viewMode === 'grid-poster' || s.viewMode === 'grid-cover'}
						<div class="grid {s.viewMode === 'grid-cover' ? 'grid-cols-3' : 'grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6'} {s.viewMode === 'grid-cover' ? 'gap-2' : 'gap-4'}">
							{#each s.filteredMovieGroups as group}
								{@const effMovie = s.getEffectiveMovie(group.primaryResult.file_path, group.primaryResult.movie)}
								<ReviewGridCard
									movieGroup={group}
									isSelected={group.primaryResult.result_id === s.currentResult?.result_id}
									isEdited={s.editedMovies.has(group.primaryResult.file_path)}
									isBulkSelected={s.selectedMovieIds.has(group.movieId)}
									selectionMode={s.selectionMode}
									effectiveMovie={effMovie ?? undefined}
									displayPosterUrl={(() => {
										if (!effMovie) return undefined;
										return s.resolvePosterUrl(effMovie, group.primaryResult.file_path);
									})()}
									displayCoverUrl={effMovie?.cover_url}
									displayImageType={s.viewMode === 'grid-cover' ? 'cover' : 'poster'}
									previewImageURL={s.reviewPageController.previewImageURL}
									onclick={(e) => {
										if (s.selectionMode) {
											s.toggleMovieSelection(group.movieId, e.shiftKey);
										} else {
											s.currentMovieIndex = s.movieGroups.findIndex(g => g.movieId === group.movieId);
											s.viewMode = 'detail';
										}
									}}
									completenessConfig={s.completenessConfig}
								/>
							{/each}
						</div>
					{:else}
						{#key s.currentResult.file_path}
							<div class="grid grid-cols-1 lg:grid-cols-[300px_1fr] gap-6" in:fade|local={{ duration: 180 }}>
							<ReviewMediaSidebar
								currentMovie={s.currentMovie}
								displayPosterUrl={s.displayPosterUrl}
								showPosterPanel={s.showPosterPanel}
								showCoverPanel={s.showCoverPanel}
								showTrailerPanel={s.showTrailerPanel}
								showScreenshotsPanel={s.showScreenshotsPanel}
								bind:showAllSidebarScreenshots={s.showAllSidebarScreenshots}
								bind:showTrailerModal={s.showTrailerModal}
								onOpenPosterCropModal={s.posterCropController.openPosterCropModal}
								onOpenCoverViewer={s.reviewPageController.openCoverViewer}
								onOpenScreenshotViewer={s.reviewPageController.openScreenshotViewer}
								onUseScreenshotAsPoster={s.useScreenshotAsPoster}
								onResetPoster={s.resetPoster}
								previewImageURL={s.reviewPageController.previewImageURL}
							/>

						<div class="space-y-6 min-w-0">
							<MovieNavigationCard
								bind:currentMovieIndex={s.currentMovieIndex}
								movieResultsLength={s.movieResults.length}
								currentMovieId={s.currentMovie.id}
								hasChanges={s.reviewPageController.hasChanges(s.currentResult.file_path)}
								onExclude={s.reviewPageController.excludeCurrentMovie}
							/>

							<SourceFilesCard
								sourceResults={s.currentMovieGroup?.results || [s.currentResult]}
								primaryFilePath={s.currentResult.file_path}
								bind:showFullSourcePath={s.showFullSourcePath}
							/>

							{#if s.canOrganize}
								<DestinationSettingsCard
									bind:destinationPath={s.destinationPath}
									bind:organizeOperation={s.organizeOperation}
								preview={s.preview}
								previewNeedsDestination={s.previewNeedsDestination}
									effectiveOperationMode={s.getEffectiveOperationMode()}
									bind:showAllPreviewScreenshots={s.showAllPreviewScreenshots}
									bind:skipNfo={s.skipNfo}
									bind:skipDownload={s.skipDownload}
									onOpenDestinationBrowser={s.reviewPageController.openDestinationBrowser}
								/>
							{/if}

							<MovieMetadataCard
								currentMovie={s.currentMovie}
								currentResult={s.currentResult}
								bind:showFieldScraperSources={s.showFieldScraperSources}
								isRescraping={s.rescrapingStates.get(s.currentResult?.result_id || '') || false}
								onOpenRescrape={() => s.currentResult && s.openRescrapeModal(s.currentResult.result_id)}
								onResetCurrentMovie={s.resetCurrentMovie}
								onUpdateCurrentMovie={s.updateCurrentMovie}
							/>

							<Card class="p-6">
								<ActressEditor
									movie={s.currentMovie!}
									onUpdate={s.updateCurrentMovie}
									actressSources={s.currentResult.actress_sources}
									showFieldSources={s.showFieldScraperSources}
								/>
							</Card>

							<ImagesMediaCard
								showScreenshotsPanel={s.showScreenshotsPanel}
								bind:showImagePanelContent={s.showImagePanelContent}
								currentMovie={s.currentMovie}
								currentResult={s.currentResult}
								displayPosterUrl={s.displayPosterUrl}
								showFieldScraperSources={s.showFieldScraperSources}
								onUpdateCurrentMovie={s.updateCurrentMovie}
								onUseScreenshotAsPoster={s.useScreenshotAsPoster}
							/>

							{#if s.canOrganize}
								<ReviewActionBar
									isUpdateMode={s.isUpdateMode}
									organizing={s.organizing}
									destinationPath={s.destinationPath}
									movieResultsLength={s.movieResults.length}
									onCancel={() => goto('/browse')}
									onOrganizeAll={s.organizeAll}
								/>
							{/if}
						</div>
						</div>
					{/key}
					{/if}
				{:else if activeTab === 'movies'}
					<Card class="p-6">
						<div class="text-center text-muted-foreground">
							<p>No movies to review yet.</p>
						</div>
					</Card>
				{/if}
			</div>

			<div
				role="tabpanel"
				id="failed-panel"
				aria-labelledby="failed-tab"
				hidden={activeTab !== 'failed'}
				class="space-y-6"
			>
				{#if activeTab === 'failed'}
					{#if hasFailed}
						<UnidentifiedFilesCard
							failedResults={s.failedResults}
							onSearchManually={s.openRescrapeModalForFailed}
						/>
						<div class="text-center">
							<Button onclick={() => goto('/browse')}>
								{#snippet children()}
									<ChevronLeft class="h-4 w-4 mr-2" />
									Back to Browse
								{/snippet}
							</Button>
						</div>
					{:else}
						<Card class="p-6">
							<div class="text-center text-muted-foreground">
								<p>No failed files. Everything was identified successfully.</p>
							</div>
						</Card>
					{/if}
				{/if}
			</div>
		{/if}
	</div>
</div>

<VideoModal
	bind:show={s.showTrailerModal}
	videoUrl={s.currentMovie?.trailer_url ?? ''}
	title="Trailer"
	onClose={() => (s.showTrailerModal = false)}
/>

<ImageViewer
	bind:show={s.showImageViewer}
	images={s.imageViewerImages}
	initialIndex={s.imageViewerIndex}
	title={s.imageViewerTitle}
	onClose={s.reviewPageController.closeImageViewer}
/>

<PosterCropModal
	bind:show={s.showPosterCropModal}
	posterCropSaving={s.posterCropSaving}
	posterCropLoadError={s.posterCropLoadError}
	cropSourceURL={s.cropSourceURL}
	cropMetrics={s.cropMetrics}
	cropBox={s.cropBox}
	overlayStyle={s.posterCropController.getPosterCropOverlayStyle()}
	configMaxPosterHeight={s.config?.output?.max_poster_height ?? 0}
	maxPosterHeight={s.maxPosterHeight}
	onClose={s.posterCropController.closePosterCropModal}
	onReset={s.posterCropController.resetPosterCropBox}
	onApply={s.posterCropController.applyPosterCrop}
	onImageLoad={s.posterCropController.handlePosterCropImageLoad}
	onImageError={s.posterCropController.handlePosterCropImageError}
	onCropMouseDown={s.posterCropController.startPosterCropDrag}
	onMaxPosterHeightChange={(v) => { s.maxPosterHeight = v; }}
/>

<RescrapeModal
	bind:show={s.showRescrapeModal}
	rescraping={s.rescrapingStates.get(s.rescrapeResultId) || false || s.bulkRescraping}
	rescrapeMovieId={s.rescrapeMovieId}
	rescrapeMovieName={s.currentMovie?.id}
	bulkMovieCount={s.bulkRescrapeMovieIds.length || undefined}
	availableScrapers={s.availableScrapers}
	bind:selectedScrapers={s.rescrapeSelectedScrapers}
	bind:manualSearchMode={s.manualSearchMode}
	bind:manualSearchInput={s.manualSearchInput}
	bind:rescrapePreset={s.rescrapePreset}
	bind:rescrapeScalarStrategy={s.rescrapeScalarStrategy}
	onApplyPreset={(preset) => s.applyRescrapePreset(preset)}
	onExecute={s.bulkRescrapeMovieIds.length > 0 ? s.executeBulkRescrape : s.executeRescrape}
/>

<DestinationBrowserModal
	bind:show={s.showDestinationBrowser}
	destinationPath={s.destinationPath}
	bind:tempDestinationPath={s.tempDestinationPath}
	onCancel={s.reviewPageController.cancelDestination}
	onConfirm={s.reviewPageController.confirmDestination}
/>

<BulkRescrapeProgress
	progress={s.bulkRescrapeProgress}
	active={s.bulkRescraping}
	onDismiss={() => { s.dismissBulkRescrapeProgress(); }}
/>
