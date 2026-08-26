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

export interface PosterAssetIdentity {
	revision: number;
	fingerprint: string;
}

interface PosterCropControllerDeps {
	getBrowser: () => boolean;
	getJobId: () => string;
	isCurrentOperation?: (jobId: string, generation: number) => boolean;
	getRouteGeneration?: () => number;
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
	getCropAssetIdentity?: () => Promise<PosterAssetIdentity | null>;
	prepareCropAsset?: (
		sourceURL: string,
	) => Promise<{ displayURL: string; identity: PosterAssetIdentity }>;
	releaseCropAsset?: (displayURL: string) => void;
	applyPosterFromUrlAsync: (resultId: string, url: string) => Promise<void>;
	mutatePosterCropAsync: (
		jobId: string,
		resultId: string,
		crop: PosterCropBox,
		maxPosterHeight?: number,
		identity?: PosterAssetIdentity,
	) => Promise<void>;
	setCropApplying: (applying: boolean) => void;
	now?: () => number;
}

export function createPosterCropController(deps: PosterCropControllerDeps) {
	const now = deps.now ?? Date.now;
	let cropAssetIdentity: PosterAssetIdentity | null = null;
	let cropAssetPreparing = false;
	let cropAssetGeneration = 0;
	let cropDisplayURL: string | null = null;

	function discardPreparedCropAsset() {
		cropAssetGeneration += 1;
		cropAssetPreparing = false;
		cropAssetIdentity = null;
		if (cropDisplayURL) {
			deps.releaseCropAsset?.(cropDisplayURL);
			cropDisplayURL = null;
		}
	}

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
		// When the production loader is preparing a blob URL, the placeholder
		// image must not become the measured crop source. The eventual load event
		// for the fetched response is the only one that establishes geometry.
		if (cropAssetPreparing) return;
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

	function getLegacyPosterCropSourceURL(sourceURL: string): string | null {
		if (!sourceURL.includes('-full.jpg')) return null;
		const currentMovie = deps.getCurrentMovie();
		if (!currentMovie) return null;
		const posterMovieId =
			deps.getCurrentResult()?.movie?.id || deps.getCurrentResult()?.movie_id || currentMovie.id;
		const fallbackURL = `/api/v1/temp/posters/${deps.getJobId()}/${posterMovieId}.jpg${sessionParam()}`;
		return `${fallbackURL}${fallbackURL.includes('?') ? '&' : '?'}v=${now()}`;
	}

	function handlePosterCropImageError() {
		const fallbackURL = getLegacyPosterCropSourceURL(deps.getCropSourceURL());
		if (fallbackURL) {
			deps.setCropSourceURL(fallbackURL);
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
		// POSTER-WRITE-HARDENING D9/D14: crop geometry is ALWAYS measured
		// against the installed bytes (the served temp poster), never a
		// divergent remote URL — the server revalidates revision/fingerprint
		// against installed state at commit time (the /temp/image?url=
		// remote-measure lane is deleted). A form-edited-but-unsaved
		// poster_url is NOT installed bytes either: refuse to measure the
		// stale installed image under a new URL (codex P2) and guide the
		// user to install the URL first via poster-from-url.
		// Effective poster source folds poster_url → cover_url fallback (the
		// same selection the downloader + apply boundary use; codex r14-C):
		// a cover-only edit is still a poster source change in waiting.
		const effectiveSourceOf = (m: { poster_url?: string; cover_url?: string }) =>
			m.poster_url || m.cover_url || '';
		const currentEffective = effectiveSourceOf(currentMovie);
		const storedEffective = currentResult?.movie ? effectiveSourceOf(currentResult.movie) : '';
		// Cropping measures the INSTALLED bytes — a cleared source (user
		// emptied the URL fields) would otherwise crop the old image and
		// commit geometry for a source the user already discarded.
		if (currentEffective !== storedEffective) {
			deps.setCropSourceURL('');
			deps.setPosterCropLoadError(
				'Poster URL was changed but not applied. Apply it with "Poster from URL" first, then crop.',
			);
			deps.setCropMetrics(null);
			deps.setCropBox(null);
			deps.setShowPosterCropModal(true);
			return;
		}
		const posterMovieId = currentResult?.movie?.id || currentResult?.movie_id || currentMovie.id;
		const sourceURL = `/api/v1/temp/posters/${deps.getJobId()}/${posterMovieId}-full.jpg${sessionParam()}`;
		const resolvedSourceURL = `${sourceURL}${sourceURL.includes('?') ? '&' : '?'}v=${now()}`;
		discardPreparedCropAsset();
		cropAssetPreparing = Boolean(deps.prepareCropAsset);
		deps.setCropSourceURL(
			deps.prepareCropAsset
				? 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs='
				: resolvedSourceURL,
		);
		deps.setPosterCropLoadError(null);
		deps.setCropMetrics(null);
		deps.setCropBox(null);
		deps.setMaxPosterHeight(null);
		deps.setCropImageElement(null);
		deps.setCropDragState(null);
		deps.setShowPosterCropModal(true);

		if (deps.prepareCropAsset) {
			const generation = cropAssetGeneration;
			const prepareCropAsset = deps.prepareCropAsset;
			const installPreparedCropAsset = (asset: {
				displayURL: string;
				identity: PosterAssetIdentity;
			}) => {
				if (generation !== cropAssetGeneration) {
					deps.releaseCropAsset?.(asset.displayURL);
					return;
				}
				cropAssetIdentity = asset.identity;
				cropAssetPreparing = false;
				cropDisplayURL = asset.displayURL;
				deps.setCropSourceURL(asset.displayURL);
			};
			const failPreparedCropAsset = () => {
				if (generation !== cropAssetGeneration) return;
				cropAssetPreparing = false;
				cropAssetIdentity = null;
				deps.setPosterCropLoadError(
					'Unable to load the installed poster source for manual cropping',
				);
				deps.setCropMetrics(null);
				deps.setCropBox(null);
			};
			const prepare = (sourceURLToPrepare: string, allowLegacyFallback: boolean) => {
				void prepareCropAsset(sourceURLToPrepare)
					.then(installPreparedCropAsset)
					.catch(() => {
						if (generation !== cropAssetGeneration) return;
						const fallbackURL = allowLegacyFallback
							? getLegacyPosterCropSourceURL(resolvedSourceURL)
							: null;
						if (fallbackURL) {
							prepare(fallbackURL, false);
							return;
						}
						failPreparedCropAsset();
					});
			};
			prepare(resolvedSourceURL, true);
		}
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
		discardPreparedCropAsset();
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

		const operationJobId = deps.getJobId();
		const operationGeneration = deps.getRouteGeneration?.() ?? 0;
		const isCurrentOperation = () =>
			!deps.isCurrentOperation || deps.isCurrentOperation(operationJobId, operationGeneration);
		deps.setCropApplying(true);
		try {
			// If the poster URL was edited client-side (not yet persisted to the
			// server), persist it first so the crop endpoint operates on the
			// edited image ({movieId}-full.jpg) rather than the stale scraped
			// poster that still lives server-side. Without this, the crop modal
			// shows the edited URL (via the image proxy) but the backend would
			// crop the original scraped image, reverting the preview.
			const serverPosterUrl = currentResult.movie?.poster_url;
			if (
				currentMovie.poster_url &&
				serverPosterUrl &&
				currentMovie.poster_url !== serverPosterUrl
			) {
				await deps.applyPosterFromUrlAsync(currentResult.result_id, currentMovie.poster_url);
				if (!isCurrentOperation()) return;
			}

			const maxPosterHeight = deps.getMaxPosterHeight();
			let identity = cropAssetIdentity;
			if (deps.prepareCropAsset) {
				if (cropAssetPreparing || !identity) {
					deps.setPosterCropLoadError(
						'Unable to verify the displayed poster source. Reopen the crop and try again.',
					);
					return;
				}
			} else if (deps.getCropAssetIdentity) {
				try {
					identity = await deps.getCropAssetIdentity();
					if (!isCurrentOperation()) return;
				} catch {
					deps.setPosterCropLoadError(
						'Unable to verify the installed poster source. Reopen the crop and try again.',
					);
					return;
				}
				if (!identity) {
					deps.setPosterCropLoadError(
						'Unable to verify the installed poster source. Reopen the crop and try again.',
					);
					return;
				}
			}
			if (!isCurrentOperation()) return;
			if (identity) {
				await deps.mutatePosterCropAsync(
					deps.getJobId(),
					currentResult.result_id,
					cropBoxVal,
					maxPosterHeight ?? undefined,
					identity,
				);
			} else {
				await deps.mutatePosterCropAsync(
					deps.getJobId(),
					currentResult.result_id,
					cropBoxVal,
					maxPosterHeight ?? undefined,
				);
			}
		} catch {
			// Errors are surfaced via toasts in the mutation handlers; abort the flow.
		} finally {
			if (isCurrentOperation()) deps.setCropApplying(false);
		}
	}

	function handleWindowResize() {
		if (!deps.getShowPosterCropModal()) return;
		refreshPosterCropMetrics();
	}

	function cleanup() {
		stopPosterCropDrag();
		discardPreparedCropAsset();
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
