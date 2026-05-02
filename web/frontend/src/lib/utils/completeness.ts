import type { Movie, CompletenessConfig } from '$lib/api/types';

export type CompletenessTier = 'incomplete' | 'partial' | 'complete';

export interface FieldCategory {
	name: string;
	filled: boolean;
	weight: number;
	tier: 'essential' | 'important' | 'nice-to-have';
}

export interface CompletenessResult {
	score: number;
	tier: CompletenessTier;
	breakdown: FieldCategory[];
}

function isFilledString(value: string | undefined): boolean {
	return typeof value === 'string' && value.trim().length > 0;
}

function isFilledNumber(value: number | undefined): boolean {
	return typeof value === 'number' && Number.isFinite(value) && value > 0;
}

type FieldChecker = (movie: Movie) => boolean;

const FIELD_CHECKERS: Record<string, FieldChecker> = {
	title: (m) => isFilledString(m.title),
	poster_url: (m) => isFilledString(m.poster_url),
	cover_url: (m) => isFilledString(m.cover_url),
	actresses: (m) => (m.actresses?.length ?? 0) > 0,
	genres: (m) => (m.genres?.length ?? 0) > 0,
	description: (m) => isFilledString(m.description),
	maker: (m) => isFilledString(m.maker),
	release_date: (m) => isFilledString(m.release_date) || isFilledNumber(m.release_year),
	director: (m) => isFilledString(m.director),
	runtime: (m) => isFilledNumber(m.runtime),
	trailer_url: (m) => isFilledString(m.trailer_url),
	screenshot_urls: (m) => (m.screenshot_urls?.length ?? 0) > 0,
	label: (m) => isFilledString(m.label),
	series: (m) => isFilledString(m.series),
	rating_score: (m) => isFilledNumber(m.rating_score),
	original_title: (m) => isFilledString(m.original_title),
	translations: (m) => (m.translations?.length ?? 0) > 0,
};

const FIELD_DISPLAY_NAMES: Record<string, string> = {
	title: 'Title',
	poster_url: 'Poster',
	cover_url: 'Cover',
	actresses: 'Actresses',
	genres: 'Genres',
	description: 'Description',
	maker: 'Maker',
	release_date: 'Release Date',
	director: 'Director',
	runtime: 'Runtime',
	trailer_url: 'Trailer',
	screenshot_urls: 'Screenshots',
	label: 'Label',
	series: 'Series',
	rating_score: 'Rating',
	original_title: 'Original Title',
	translations: 'Translations',
};

const DEFAULT_TIERS = {
	essential: { weight: 50, fields: ['title', 'poster_url', 'cover_url', 'actresses', 'genres'] },
	important: { weight: 35, fields: ['description', 'maker', 'release_date', 'director', 'runtime', 'trailer_url', 'screenshot_urls'] },
	nice_to_have: { weight: 15, fields: ['label', 'series', 'rating_score', 'original_title', 'translations'] }
};

function getScreenshotFill(movie: Movie): number {
	const screenshots = movie.screenshot_urls ?? [];
	if (screenshots.length === 0) return 0;
	if (screenshots.length <= 2) return 0.5;
	return 1;
}

const TIER_NAME_MAP: Record<string, 'essential' | 'important' | 'nice-to-have'> = {
	essential: 'essential',
	important: 'important',
	nice_to_have: 'nice-to-have',
};

export function calculateCompleteness(movie: Movie, config?: CompletenessConfig): CompletenessResult {
	const tiers = (config?.enabled ? config.tiers : DEFAULT_TIERS) as typeof DEFAULT_TIERS;

	const breakdown: FieldCategory[] = [];

	for (const [tierName, tierDef] of Object.entries(tiers)) {
		const mappedTier = TIER_NAME_MAP[tierName] ?? (tierName as 'essential' | 'important' | 'nice-to-have');
		const fieldWeight = tierDef.fields.length > 0
			? (tierDef.weight / 100) / tierDef.fields.length
			: 0;
		for (const fieldName of tierDef.fields) {
			const checker = FIELD_CHECKERS[fieldName];
			breakdown.push({
				name: FIELD_DISPLAY_NAMES[fieldName] ?? fieldName,
				filled: checker ? checker(movie) : false,
				weight: fieldWeight,
				tier: mappedTier,
			});
		}
	}

	const screenshotFill = getScreenshotFill(movie);

	const filledWeight = breakdown.reduce((sum, cat) => {
		if (cat.name === 'Screenshots') {
			return sum + cat.weight * screenshotFill;
		}
		return sum + (cat.filled ? cat.weight : 0);
	}, 0);

	const totalWeight = breakdown.reduce((sum, cat) => sum + cat.weight, 0);
	const rawScore = totalWeight > 0 ? (filledWeight / totalWeight) * 100 : 0;
	const score = Math.round(rawScore);

	let tier: CompletenessTier;
	if (score < 40) {
		tier = 'incomplete';
	} else if (score < 80) {
		tier = 'partial';
	} else {
		tier = 'complete';
	}

	return { score, tier, breakdown };
}
