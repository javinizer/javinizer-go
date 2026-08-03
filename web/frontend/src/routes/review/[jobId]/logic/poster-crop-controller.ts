import type { FileResult, Movie } from '$lib/api/types';
import { BaseClient } from '$lib/api/clients/common';

function sessionParam(): string {
	const sid = BaseClient.getSessionID();
	return sid ? `?session=${encodeURIComponent(sid)}` : '';
}

import {
	clamp,
	getDefaultPosterCropBox,
	normalizeCropBox,
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
	mutatePosterCropAsync: (jobId: string, resultId: string, crop: PosterCropBox, maxPosterHeight?: number, expectedSourceURL?: string, expectedPosterRevision?: string) => Promise<void>;
	setCropApplying: (applying: boolean) => void;
	// fetchPosterRevision returns the temp poster endpoint's X-Poster-Revision
	// header (cache generation token) for a -full.jpg crop source URL, WITHOUT
	// downloading the image (HEAD). Optional: absent → legacy URL-only guard.
	fetchPosterRevision?: (url: string) => Promise<string>;
	now?: () => number;
}

export function createPosterCropController(deps: PosterCropControllerDeps) {
	const now = deps.now ?? Date.now;

	// The (requestURL, effectiveSource) pair of the CURRENTLY DISPLAYED crop
	// image, captured at the moment the image request is ISSUED — NOT at
	// image-load time: a job refetch or navigation can swap the reactive
	// currentMovie A→B between setCropSourceURL(A) and A's load event, and a
	// load-time capture would bind B's source to A's coordinate space. The
	// crop box, metrics and cropSourceURL all describe that issued image, so
	// applyPosterCrop MUST submit this captured pair (not the recomputed live
	// source) as expected_source_url — otherwise the server 409 guard
	// validates stale A coordinates against B and passes. `revision` fills in
	// asynchronously from the temp poster endpoint's X-Poster-Revision: a
	// same-URL cache refresh (rescrape / poster-from-URL) leaves every URL
	// equal while swapping the generation the coordinates were measured on.
	interface CropSourceToken {
		requestURL: string;
		effectiveSource: string;
		revision: string;
	}
	let cropSourceToken: CropSourceToken | null = null;

	// Issue a crop image request and bind the captured pair to it. Only the
	// {id}-full.jpg cache asset carries a server-side revision worth
	// validating (the URL-image proxy and the preview-fallback {id}.jpg do
	// not); a stale revision resolution arriving after a NEWER request
	// superseded this token is dropped.
	function issueCropImageRequest(sourceURL: string) {
		deps.setCropSourceURL(sourceURL);
		const movie = deps.getCurrentMovie();
		const token: CropSourceToken = {
			requestURL: sourceURL,
			effectiveSource: movie ? movie.poster_url || movie.cover_url || '' : '',
			revision: '',
		};
		cropSourceToken = token;
		if (deps.fetchPosterRevision && sourceURL.includes('-full.jpg')) {
			deps.fetchPosterRevision(sourceURL)
				.then((rev) => {
					if (cropSourceToken === token && rev) token.revision = rev;
				})
				.catch(() => {
					// Revision fetch failed → empty revision → legacy URL-only guard.
				});
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

		// The (requestURL, effectiveSource) guard pair for the displayed image
		// was captured when the request was ISSUED (issueCropImageRequest) —
		// re-reading the reactive movie HERE would pair whatever source is live
		// at load time (possibly a post-refetch B) with image A's dimensions.

		const currentResult = deps.getCurrentResult();

		// Prefer the AUTHORITATIVE server-stored crop over LOCAL geometry: a job
		// refetch may have supplied NEWER poster_crop_bounds than this browser's
		// local posterCropStates entry (another tab/device re-cropped after this
		// tab last saved) — seeding from the stale LOCAL entry would reopen the
		// older rectangle and an unchanged Apply would overwrite the newer
		// persisted crop. The bounds were measured against the dims recorded on
		// them, so normalize there and restore onto the CURRENT source dims
		// (ratio-based, so it stays correct if the image was re-rendered at a
		// different size); bounds without usable dims predate dimension
		// recording. LOCAL state remains the fallback when the server carries no
		// recorded crop — and for OWN edits both rules agree: a save overlays
		// the response bounds into the results AND writes the normalized box
		// into the local map (asserted by the identical-geometry test).
		let seedCrop: PosterCropState | undefined;
		const bounds = deps.getCurrentMovie()?.poster_crop_bounds;
		const boundsWidth = bounds?.image_width ?? 0;
		const boundsHeight = bounds?.image_height ?? 0;
		if (bounds && boundsWidth > 0 && boundsHeight > 0) {
			seedCrop = normalizeCropBox(
				{ x: bounds.x, y: bounds.y, width: bounds.width, height: bounds.height },
				{
					sourceWidth: boundsWidth,
					sourceHeight: boundsHeight,
					displayWidth: 0,
					displayHeight: 0,
					imageOffsetX: 0,
					imageOffsetY: 0,
				},
			);
		} else if (currentResult) {
			// No usable SERVER geometry: fall back to this browser's local crop
			// state (own unsaved drags within the session).
			seedCrop = deps.getPosterCropStates().get(currentResult.file_path);
		}

		deps.setCropBox(
			seedCrop
				? restoreCropBox(seedCrop, sourceWidth, sourceHeight)
				: getDefaultPosterCropBox(sourceWidth, sourceHeight),
		);

		refreshPosterCropMetrics();
	}

	function handlePosterCropImageError() {
		const currentMovie = deps.getCurrentMovie();
		if (currentMovie && deps.getCropSourceURL().includes('-full.jpg')) {
			const posterMovieId = deps.getCurrentResult()?.movie_id ?? currentMovie.id;
			const fallbackURL = `/api/v1/temp/posters/${deps.getJobId()}/${posterMovieId}.jpg${sessionParam()}`;
			// The fallback is an image RE-REQUEST: re-bind the guard pair to it.
			issueCropImageRequest(`${fallbackURL}${fallbackURL.includes('?') ? '&' : '?'}v=${now()}`);
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
		// The backend crops the EFFECTIVE poster source (effectivePosterSourceOf:
		// poster_url when set, else cover_url), so an unsaved SOURCE edit must be
		// detected against that same pair — a cover-backed movie has an EMPTY
		// poster_url, and a poster_url-only check would miss an unsaved cover_url
		// edit entirely: the modal would show the cached OLD cover image while
		// the pre-apply sync below (same rule) persists the new one, or neither
		// sees the change and the crop silently targets the cover the user no
		// longer references. Server-side drift comparison is the effective pair
		// too (a same-poster_url cover-only edit must surface). Exact string
		// compare mirrors the backend's pre/post-lock source check.
		const editedSource = currentMovie.poster_url || currentMovie.cover_url || '';
		const serverSource = currentResult?.movie
			? (currentResult.movie.poster_url || currentResult.movie.cover_url || '')
			: '';
		let sourceURL: string;
		if (editedSource && currentResult?.movie && editedSource !== serverSource) {
			sourceURL = `/api/v1/temp/image?url=${encodeURIComponent(editedSource)}${sessionParam().replace('?', '&')}`;
		} else {
			const posterMovieId = currentResult?.movie_id ?? currentMovie.id;
			const fullPosterURL = `/api/v1/temp/posters/${deps.getJobId()}/${posterMovieId}-full.jpg${sessionParam()}`;
			sourceURL = fullPosterURL;
		}
		// Bind the guard pair NOW, at request-issue time — the image loads
		// asynchronously and the reactive movie may change in between.
		issueCropImageRequest(`${sourceURL}${sourceURL.includes('?') ? '&' : '?'}v=${now()}`);
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
			//
			// The server poster_url may be EMPTY (cover-backed movie) while the
			// edited URL is set: pre-sync anyway. The same holds when the
			// UNSAVED edit landed on cover_url — the effective source (poster
			// ?? cover) is what the backend crops (effectivePosterSourceOf),
			// so the drift check, like openPosterCropModal's preview source,
			// runs on the effective pair, not poster_url alone. poster-from-URL
			// handles an empty prior source (it replaces {movieId}-full.jpg
			// wholesale and returns the same { cropped_poster_url, poster_url }
			// shape as the non-cover path), and openPosterCropModal already
			// shows the edited URL in this case — skipping the sync would crop
			// the cover with bounds measured on the edited image.
			const editedSource = currentMovie.poster_url || currentMovie.cover_url || '';
			const serverSource =
				(currentResult.movie?.poster_url || currentResult.movie?.cover_url) ?? '';
			if (editedSource && editedSource !== serverSource) {
				await deps.applyPosterFromUrlAsync(currentResult.result_id, editedSource);
			}

			const maxPosterHeight = deps.getMaxPosterHeight();
			// Tell the server which source these coordinates were measured
			// against — the pair CAPTURED when the displayed image's request was
			// ISSUED, not the live movie's recomputed effective source. If a job
			// refetch swapped poster A→B while this modal stayed open, the box and
			// cropSourceURL still describe A; sending the captured A here makes
			// the server-side 409 guard actually fire (mismatch vs B) instead of
			// silently validating stale A coordinates against the new source —
			// the tokenized 409 toast then tells the user to reload and
			// re-measure. The revision alongside catches the stealthier same-URL
			// swap (rescrape / poster-from-URL refreshed {id}-full.jpg's bytes
			// while the URL stayed equal). The token only describes the image it
			// was issued with — if cropSourceURL moved on since, fall back to the
			// live editedSource (identical to the pre-fix behavior: after any
			// pre-sync above the server source IS editedSource).
			const issued = cropSourceToken && cropSourceToken.requestURL === deps.getCropSourceURL()
				? cropSourceToken
				: null;
			const expectedSource = issued?.effectiveSource || editedSource;
			await deps.mutatePosterCropAsync(deps.getJobId(), currentResult.result_id, cropBoxVal, maxPosterHeight ?? undefined, expectedSource || undefined, issued?.revision || undefined);
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
