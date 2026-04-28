import type { BatchJobResponse, Movie } from '$lib/api/types';

interface ReviewPageControllerDeps {
	getJob: () => BatchJobResponse | null;
	getCurrentMovie: () => Movie | null;
	getEditedMovies: () => Map<string, Movie>;
	getDestinationPath: () => string;
	setDestinationPath: (destinationPath: string) => void;
	getTempDestinationPath: () => string;
	setTempDestinationPath: (destinationPath: string) => void;
	setShowDestinationBrowser: (show: boolean) => void;
	setShowImageViewer: (show: boolean) => void;
	setImageViewerImages: (images: string[]) => void;
	setImageViewerIndex: (index: number) => void;
	setImageViewerTitle: (title: string | undefined) => void;
	excludeMovie: (jobId: string, movieId: string) => void;
	api: {
		getPreviewImageURL: (url: string) => string;
	};
}

export function createReviewPageController(deps: ReviewPageControllerDeps) {
	function hasChanges(filePath: string): boolean {
		return deps.getEditedMovies().has(filePath);
	}

	function openDestinationBrowser() {
		deps.setTempDestinationPath(deps.getDestinationPath());
		deps.setShowDestinationBrowser(true);
	}

	function confirmDestination() {
		deps.setDestinationPath(deps.getTempDestinationPath());
		deps.setShowDestinationBrowser(false);
	}

	function cancelDestination() {
		deps.setShowDestinationBrowser(false);
	}

	function previewImageURL(url: string | undefined): string {
		if (!url) return '';
		if (url.startsWith('/api/v1/')) return url;
		if (url.startsWith('/')) return url;
		return deps.api.getPreviewImageURL(url);
	}

	function openScreenshotViewer(index: number) {
		const currentMovie = deps.getCurrentMovie();
		if (!currentMovie?.screenshot_urls) return;
		deps.setImageViewerImages(currentMovie.screenshot_urls.map((url) => previewImageURL(url)));
		deps.setImageViewerIndex(index);
		deps.setImageViewerTitle(undefined);
		deps.setShowImageViewer(true);
	}

	function openCoverViewer() {
		const currentMovie = deps.getCurrentMovie();
		if (!currentMovie?.cover_url) return;
		deps.setImageViewerImages([previewImageURL(currentMovie.cover_url)]);
		deps.setImageViewerIndex(0);
		deps.setImageViewerTitle('Cover/Fanart');
		deps.setShowImageViewer(true);
	}

	function closeImageViewer() {
		deps.setShowImageViewer(false);
	}

	function excludeCurrentMovie() {
		const currentMovie = deps.getCurrentMovie();
		const job = deps.getJob();
		if (!currentMovie || !job) return;

		deps.excludeMovie(job.id, currentMovie.id);
	}

	return {
		hasChanges,
		openDestinationBrowser,
		confirmDestination,
		cancelDestination,
		previewImageURL,
		openScreenshotViewer,
		openCoverViewer,
		closeImageViewer,
		excludeCurrentMovie
	};
}
