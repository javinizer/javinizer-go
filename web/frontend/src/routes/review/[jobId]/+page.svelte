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
	import SourceFilesCard from './components/SourceFilesCard.svelte';
	import { createReviewState } from './stores/review-state.svelte';
	import {
		ChevronLeft,
		CircleAlert
	} from 'lucide-svelte';

	const s = createReviewState($page);
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
		{:else if s.job && s.movieResults.length === 0}
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
		{:else if s.currentMovie && s.currentResult}
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

			{#if s.viewMode === 'grid'}
				<div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4">
					{#each s.movieGroups as group, index}
						<ReviewGridCard
							movieGroup={group}
							isSelected={index === s.currentMovieIndex}
							isEdited={s.editedMovies.has(group.primaryResult.file_path)}
							displayPosterUrl={(() => {
								const movie = group.primaryResult.data;
								if (!movie) return undefined;
								return s.resolvePosterUrl(movie, group.primaryResult.file_path);
							})()}
							previewImageURL={s.reviewPageController.previewImageURL}
							onclick={() => {
								s.currentMovieIndex = index;
								s.viewMode = 'detail';
							}}
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
						isRescraping={s.rescrapingStates.get(s.currentResult?.movie_id || '') || false}
						onOpenRescrape={() => s.currentResult && s.openRescrapeModal(s.currentResult.movie_id)}
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
	posterCropSaving={s.posterCropMutation.isPending}
	posterCropLoadError={s.posterCropLoadError}
	cropSourceURL={s.cropSourceURL}
	cropMetrics={s.cropMetrics}
	cropBox={s.cropBox}
	overlayStyle={s.posterCropController.getPosterCropOverlayStyle()}
	onClose={s.posterCropController.closePosterCropModal}
	onReset={s.posterCropController.resetPosterCropBox}
	onApply={s.posterCropController.applyPosterCrop}
	onImageLoad={s.posterCropController.handlePosterCropImageLoad}
	onImageError={s.posterCropController.handlePosterCropImageError}
	onCropMouseDown={s.posterCropController.startPosterCropDrag}
/>

<RescrapeModal
	bind:show={s.showRescrapeModal}
	rescraping={s.rescrapingStates.get(s.rescrapeMovieId) || false}
	rescrapeMovieId={s.rescrapeMovieId}
	availableScrapers={s.availableScrapers}
	bind:selectedScrapers={s.rescrapeSelectedScrapers}
	bind:manualSearchMode={s.manualSearchMode}
	bind:manualSearchInput={s.manualSearchInput}
	bind:rescrapePreset={s.rescrapePreset}
	bind:rescrapeScalarStrategy={s.rescrapeScalarStrategy}
	onApplyPreset={(preset) => s.applyRescrapePreset(preset)}
	onExecute={s.executeRescrape}
/>

<DestinationBrowserModal
	bind:show={s.showDestinationBrowser}
	destinationPath={s.destinationPath}
	bind:tempDestinationPath={s.tempDestinationPath}
	onCancel={s.reviewPageController.cancelDestination}
	onConfirm={s.reviewPageController.confirmDestination}
/>
