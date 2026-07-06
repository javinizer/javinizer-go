import type { FileResult, Movie } from '$lib/api/types';
import { BaseClient } from '$lib/api/clients/common';

function sessionParam(): string {
	const sid = BaseClient.getSessionID();
	return sid ? `?session=${encodeURIComponent(sid)}` : '';
}

import {
	clamp,
	getDefaultPosterCropBox,
	restoreCropBox,
	type PosterCropBox,
	type PosterCropMetrics,
	type PosterCropState,
} from '../review-utils';

export interface PosterCropDragState {
	startX: number;
	startY: number;
	originX: number;
	originY: number;
}

interface PosterCropControllerDeps {
	getBrowser: () => boolean;
	getJobId: () => string;
	getCurrentMovie: () => Movie | null;
	getCurrentResult: () => FileResult | undefined;
	getShowPosterCropModal: () => boolean;
	setShowPosterCropModal: (show: boolean) => void;
	setPosterCropLoadError: (error: string | null) => void;
	getCropSourceURL: () => string;
	setCropSourceURL: (url: string) => void;
	getCropImageElement: () => HTMLImageElement | null;
	setCropImageElement: (imageElement: HTMLImageElement | null) => void;
	getCropMetrics: () => PosterCropMetrics | null;
	setCropMetrics: (metrics: PosterCropMetrics | null) => void;
	getCropBox: () => PosterCropBox | null;
	setCropBox: (box: PosterCropBox | null) => void;
	getMaxPosterHeight: () => number | null;
	setMaxPosterHeight: (height: number | null) => void;
	getCropDragState: () => PosterCropDragState | null;
	setCropDragState: (state: PosterCropDragState | null) => void;
	getPosterCropStates: () => Map<string, PosterCropState>;
	applyPosterFromUrlAsync: (resultId: string, url: string) => Promise<void>;
	mutatePosterCropAsync: (jobId: string, resultId: string, crop: PosterCropBox, maxPosterHeight?: number) => Promise<void>;
	setCropApplying: (applying: boolean) => void;
	now?: () => number;
}

export function createPosterCropController(deps: PosterCropControllerDeps) {
	const now = deps.now ?? Date.now;

	function refreshPosterCropMetrics() {
		const cropImageElement = deps.getCropImageElement();
		const cropMetrics = deps.getCropMetrics();
		if (!cropImageElement || !cropMetrics) return;

		const displayWidth = cropImageElement.clientWidth;
		const displayHeight = cropImageElement.clientHeight;
		if (displayWidth <= 0 || displayHeight <= 0) return;

		deps.setCropMetrics({
			...cropMetrics,
			displayWidth,
			displayHeight,
			imageOffsetX: cropImageElement.offsetLeft,
			imageOffsetY: cropImageElement.offsetTop,
		});
	}

	function handlePosterCropImageLoad(event: Event) {
		deps.setPosterCropLoadError(null);

		const imageElement = event.currentTarget as HTMLImageElement | null;
		if (!imageElement) return;
		deps.setCropImageElement(imageElement);

		const sourceWidth = imageElement.naturalWidth;
		const sourceHeight = imageElement.naturalHeight;
		if (sourceWidth <= 0 || sourceHeight <= 0) {
			deps.setPosterCropLoadError('Failed to read poster dimensions');
			return;
		}

		const displayWidth = imageElement.clientWidth;
		const displayHeight = imageElement.clientHeight;
		if (displayWidth <= 0 || displayHeight <= 0) {
			deps.setPosterCropLoadError('Failed to measure poster layout');
			return;
		}

		deps.setCropMetrics({
			sourceWidth,
			sourceHeight,
			displayWidth,
			displayHeight,
			imageOffsetX: imageElement.offsetLeft,
			imageOffsetY: imageElement.offsetTop,
		});

		const currentResult = deps.getCurrentResult();
		const savedCrop = currentResult
			? deps.getPosterCropStates().get(currentResult.file_path)
			: undefined;

		deps.setCropBox(
			savedCrop
				? restoreCropBox(savedCrop, sourceWidth, sourceHeight)
				: getDefaultPosterCropBox(sourceWidth, sourceHeight),
		);

		refreshPosterCropMetrics();
	}

	function handlePosterCropImageError() {
		const currentMovie = deps.getCurrentMovie();
		if (currentMovie && deps.getCropSourceURL().includes('-full.jpg')) {
			const posterMovieId = deps.getCurrentResult()?.movie_id ?? currentMovie.id;
			const fallbackURL = `/api/v1/temp/posters/${deps.getJobId()}/${posterMovieId}.jpg${sessionParam()}`;
			deps.setCropSourceURL(`${fallbackURL}?v=${now()}`);
			return;
		}

		deps.setPosterCropLoadError('Poster source is not available for manual cropping');
		deps.setCropMetrics(null);
		deps.setCropBox(null);
	}

	function openPosterCropModal() {
		const currentMovie = deps.getCurrentMovie();
		if (!currentMovie) return;

		const currentResult = deps.getCurrentResult();
		let sourceURL: string;
		if (
			currentMovie.poster_url &&
			currentResult?.movie &&
			currentMovie.poster_url !== currentResult.movie.poster_url
		) {
			sourceURL = `/api/v1/temp/image?url=${encodeURIComponent(currentMovie.poster_url)}${sessionParam().replace('?', '&')}`;
		} else {
			const posterMovieId = currentResult?.movie_id ?? currentMovie.id;
			const fullPosterURL = `/api/v1/temp/posters/${deps.getJobId()}/${posterMovieId}-full.jpg${sessionParam()}`;
			sourceURL = fullPosterURL;
		}
		deps.setCropSourceURL(`${sourceURL}?v=${now()}`);
		deps.setPosterCropLoadError(null);
		deps.setCropMetrics(null);
		deps.setCropBox(null);
		deps.setMaxPosterHeight(null);
		deps.setCropImageElement(null);
		deps.setCropDragState(null);
		deps.setShowPosterCropModal(true);
	}

	function movePosterCropBox(event: MouseEvent) {
		const cropDragState = deps.getCropDragState();
		const cropBox = deps.getCropBox();
		if (!cropDragState || !cropBox) return;

		event.preventDefault();
		refreshPosterCropMetrics();
		const cropMetrics = deps.getCropMetrics();
		if (!cropMetrics) return;

		const scaleX = cropMetrics.displayWidth / cropMetrics.sourceWidth;
		const scaleY = cropMetrics.displayHeight / cropMetrics.sourceHeight;
		if (scaleX <= 0 || scaleY <= 0) return;

		const deltaXSource = (event.clientX - cropDragState.startX) / scaleX;
		const deltaYSource = (event.clientY - cropDragState.startY) / scaleY;
		const maxX = Math.max(0, cropMetrics.sourceWidth - cropBox.width);
		const maxY = Math.max(0, cropMetrics.sourceHeight - cropBox.height);

		deps.setCropBox({
			...cropBox,
			x: clamp(Math.round(cropDragState.originX + deltaXSource), 0, maxX),
			y: clamp(Math.round(cropDragState.originY + deltaYSource), 0, maxY),
		});
	}

	function stopPosterCropDrag() {
		deps.setCropDragState(null);
		if (!deps.getBrowser()) return;
		window.removeEventListener('mousemove', movePosterCropBox);
		window.removeEventListener('mouseup', stopPosterCropDrag);
	}

	function closePosterCropModal() {
		stopPosterCropDrag();
		deps.setShowPosterCropModal(false);
	}

	function startPosterCropDrag(event: MouseEvent) {
		const cropMetrics = deps.getCropMetrics();
		const cropBox = deps.getCropBox();
		if (!deps.getBrowser() || event.button !== 0 || !cropMetrics || !cropBox) return;

		event.preventDefault();
		deps.setCropDragState({
			startX: event.clientX,
			startY: event.clientY,
			originX: cropBox.x,
			originY: cropBox.y,
		});

		window.addEventListener('mousemove', movePosterCropBox);
		window.addEventListener('mouseup', stopPosterCropDrag);
	}

	function resetPosterCropBox() {
		const cropMetrics = deps.getCropMetrics();
		if (!cropMetrics) return;
		deps.setCropBox(getDefaultPosterCropBox(cropMetrics.sourceWidth, cropMetrics.sourceHeight));
	}

	function getPosterCropOverlayStyle(): string {
		const cropMetrics = deps.getCropMetrics();
		const cropBox = deps.getCropBox();
		if (!cropMetrics || !cropBox) return '';

		const scaleX = cropMetrics.displayWidth / cropMetrics.sourceWidth;
		const scaleY = cropMetrics.displayHeight / cropMetrics.sourceHeight;
		const left = Math.round(cropMetrics.imageOffsetX + cropBox.x * scaleX);
		const top = Math.round(cropMetrics.imageOffsetY + cropBox.y * scaleY);
		const width = Math.round(cropBox.width * scaleX);
		const height = Math.round(cropBox.height * scaleY);

		return `left:${left}px;top:${top}px;width:${width}px;height:${height}px;box-shadow:0 0 0 9999px rgba(0,0,0,0.45);`;
	}

	async function applyPosterCrop() {
		const currentMovie = deps.getCurrentMovie();
		const currentResult = deps.getCurrentResult();
		const cropBoxVal = deps.getCropBox();
		if (!currentMovie || !currentResult || !cropBoxVal) return;

		deps.setCropApplying(true);
		try {
			// If the poster URL was edited client-side (not yet persisted to the
			// server), persist it first so the crop endpoint operates on the
			// edited image ({movieId}-full.jpg) rather than the stale scraped
			// poster that still lives server-side. Without this, the crop modal
			// shows the edited URL (via the image proxy) but the backend would
			// crop the original scraped image, reverting the preview.
			const serverPosterUrl = currentResult.movie?.poster_url;
			if (currentMovie.poster_url && serverPosterUrl && currentMovie.poster_url !== serverPosterUrl) {
				await deps.applyPosterFromUrlAsync(currentResult.result_id, currentMovie.poster_url);
			}

			const maxPosterHeight = deps.getMaxPosterHeight();
			await deps.mutatePosterCropAsync(deps.getJobId(), currentResult.result_id, cropBoxVal, maxPosterHeight ?? undefined);
		} catch {
			// Errors are surfaced via toasts in the mutation handlers; abort the flow.
		} finally {
			deps.setCropApplying(false);
		}
	}

	function handleWindowResize() {
		if (!deps.getShowPosterCropModal()) return;
		refreshPosterCropMetrics();
	}

	function cleanup() {
		stopPosterCropDrag();
	}

	return {
		refreshPosterCropMetrics,
		handlePosterCropImageLoad,
		handlePosterCropImageError,
		openPosterCropModal,
		closePosterCropModal,
		startPosterCropDrag,
		stopPosterCropDrag,
		resetPosterCropBox,
		getPosterCropOverlayStyle,
		applyPosterCrop,
		handleWindowResize,
		cleanup,
	};
}
