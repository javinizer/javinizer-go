import type { ActressInfo, CastNameProvenance, ScraperResult } from '$lib/api/types';
import { formatActressName, type FormatActressNameOptions } from './actress';

export function sourceCastNames(result: ScraperResult, options?: FormatActressNameOptions) {
	return (result.actresses ?? [])
		.map((actress, index) => {
			const evidence =
				result.cast_name_provenance?.length === result.actresses?.length
					? result.cast_name_provenance?.[index]
					: undefined;
			const provenance =
				evidence?.source === result.source && evidenceMatchesActress(evidence, actress)
					? evidence
					: undefined;
			// Source labels have already been decoded by the scraper. Never HTML-decode
			// here or reorder an uncertain label according to output preferences.
			const displayName =
				provenance?.display_name ||
				(actress.first_name || actress.last_name || actress.japanese_name
					? formatActressName(actress, options)
					: '');
			return { displayName, provenance, index };
		})
		.filter((entry) => entry.displayName !== '');
}

export function canCorrectSourceName(
	provenance?: CastNameProvenance | null,
): provenance is CastNameProvenance {
	return (
		!!provenance &&
		provenance.source === 'javlibrary' &&
		/^[A-Za-z0-9]{1,64}$/.test(provenance.source_id) &&
		!!normalizeNamePart(provenance.display_name) &&
		(provenance.state === 'unknown_order' || provenance.state === 'user_corrected')
	);
}

export function normalizeNamePart(value: string): string {
	// Match Go strings.Fields (Unicode White_Space), not JavaScript \s: the
	// latter omits U+0085 and incorrectly treats U+FEFF as whitespace.
	return value
		.split(/\p{White_Space}+/u)
		.filter(Boolean)
		.join(' ');
}

export function splitReconstructsSource(
	sourceName: string,
	firstName: string,
	lastName: string,
): boolean {
	const first = normalizeNamePart(firstName);
	const last = normalizeNamePart(lastName);
	const source = normalizeNamePart(sourceName);
	return !!first && !!last && (source === `${first} ${last}` || source === `${last} ${first}`);
}

// An equally sized but reordered sidecar must not authorize correcting another
// performer's source ID. This verifies label alignment, not canonical identity.
function evidenceMatchesActress(
	evidence: CastNameProvenance | null | undefined,
	actress: ActressInfo,
): boolean {
	if (!evidence) return false;
	const display = normalizeNamePart(evidence.display_name);
	if (!display) return false;
	switch (evidence.state) {
		case 'native_unsplit':
			return display === normalizeNamePart(actress.japanese_name ?? '');
		case 'single_name':
			return (
				display === normalizeNamePart(actress.first_name ?? '') &&
				!normalizeNamePart(actress.last_name ?? '')
			);
		case 'unknown_order':
		case 'user_corrected':
			return splitReconstructsSource(display, actress.first_name ?? '', actress.last_name ?? '');
		default:
			return false;
	}
}
