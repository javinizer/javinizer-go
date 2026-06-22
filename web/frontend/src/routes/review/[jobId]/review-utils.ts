export interface PosterPreviewOverride {
	url: string;
	version: number;
}

export interface PosterCropBox {
	x: number;
	y: number;
	width: number;
	height: number;
}

export interface PosterCropMetrics {
	sourceWidth: number;
	sourceHeight: number;
	displayWidth: number;
	displayHeight: number;
	imageOffsetX: number;
	imageOffsetY: number;
}

export interface PosterCropState {
	xRatio: number;
	yRatio: number;
	widthRatio: number;
	heightRatio: number;
}

const LANDSCAPE_CROP_WIDTH_RATIO = 0.472;
const POSTER_TARGET_ASPECT_RATIO = 2 / 3;

export function truncatePath(path: string, maxLength: number = 80): string {
	if (path.length <= maxLength) return path;

	const ellipsis = '...';
	const charsToShow = maxLength - ellipsis.length;
	const frontChars = Math.ceil(charsToShow * 0.4);
	const backChars = Math.floor(charsToShow * 0.6);

	return path.slice(0, frontChars) + ellipsis + path.slice(-backChars);
}

export function clamp(value: number, min: number, max: number): number {
	return Math.min(max, Math.max(min, value));
}

export function normalizeCropBox(box: PosterCropBox, metrics: PosterCropMetrics): PosterCropState {
	return {
		xRatio: box.x / metrics.sourceWidth,
		yRatio: box.y / metrics.sourceHeight,
		widthRatio: box.width / metrics.sourceWidth,
		heightRatio: box.height / metrics.sourceHeight,
	};
}

export function restoreCropBox(
	state: PosterCropState,
	sourceWidth: number,
	sourceHeight: number,
): PosterCropBox {
	const width = clamp(Math.round(state.widthRatio * sourceWidth), 1, sourceWidth);
	const height = clamp(Math.round(state.heightRatio * sourceHeight), 1, sourceHeight);
	const maxX = Math.max(0, sourceWidth - width);
	const maxY = Math.max(0, sourceHeight - height);

	return {
		x: clamp(Math.round(state.xRatio * sourceWidth), 0, maxX),
		y: clamp(Math.round(state.yRatio * sourceHeight), 0, maxY),
		width,
		height,
	};
}

export function getDefaultPosterCropBox(sourceWidth: number, sourceHeight: number): PosterCropBox {
	const sourceAspect = sourceWidth / sourceHeight;

	if (sourceAspect > 1.2) {
		const width = Math.max(1, Math.round(sourceWidth * LANDSCAPE_CROP_WIDTH_RATIO));
		return {
			x: sourceWidth - width,
			y: 0,
			width,
			height: sourceHeight,
		};
	}

	let width = sourceWidth;
	let height = sourceHeight;
	if (sourceAspect > POSTER_TARGET_ASPECT_RATIO) {
		width = Math.max(1, Math.round(sourceHeight * POSTER_TARGET_ASPECT_RATIO));
	} else {
		height = Math.max(1, Math.round(sourceWidth / POSTER_TARGET_ASPECT_RATIO));
	}

	return {
		x: Math.max(0, Math.floor((sourceWidth - width) / 2)),
		y: Math.max(0, Math.floor((sourceHeight - height) / 2)),
		width,
		height,
	};
}

// PreviewOutput describes the final pixel dimensions and aspect-ratio label
// that the Adjust Crop modal shows for a given crop box + max height cap.
export interface PreviewOutput {
	outputWidth: number;
	outputHeight: number;
	ratioLabel: string;
	willResize: boolean;
}

function gcd(a: number, b: number): number {
	return b === 0 ? a : gcd(b, a % b);
}

// computeCropPreview calculates the resulting poster dimensions after applying
// the optional max height cap (0 = no cap, preserves source resolution).
// Returns the output pixel dimensions, a simplified ratio label, and whether
// the source will be downscaled.
export function computeCropPreview(
	cropBox: PosterCropBox | null,
	maxPosterHeight: number,
): PreviewOutput {
	const empty: PreviewOutput = {
		outputWidth: 0,
		outputHeight: 0,
		ratioLabel: '',
		willResize: false,
	};
	if (!cropBox) return empty;

	const sourceWidth = cropBox.width;
	const sourceHeight = cropBox.height;
	if (sourceWidth === 0 || sourceHeight === 0) return empty;

	const effectiveMax = maxPosterHeight === 0 ? Infinity : maxPosterHeight;
	const willResize = sourceHeight > effectiveMax;

	let outputWidth: number;
	let outputHeight: number;
	if (willResize) {
		outputHeight = effectiveMax;
		outputWidth = Math.round((sourceWidth * effectiveMax) / sourceHeight);
	} else {
		outputWidth = sourceWidth;
		outputHeight = sourceHeight;
	}

	const d = gcd(outputWidth, outputHeight);
	const rw = outputWidth / d;
	const rh = outputHeight / d;
	const ratioLabel =
		rw > 20 || rh > 20 ? `${(outputWidth / outputHeight).toFixed(3)}:1` : `${rw}:${rh}`;

	return { outputWidth, outputHeight, ratioLabel, willResize };
}
