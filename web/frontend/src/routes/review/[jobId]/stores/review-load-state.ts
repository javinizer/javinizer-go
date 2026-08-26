export type ReviewLoadPhase = 'loading' | 'error' | 'refresh-error' | 'ready';

export interface ReviewLoadSnapshot {
	hasUsableJob: boolean;
	isPending: boolean;
	isFetching: boolean;
	hasError: boolean;
}

export function isAbortError(error: unknown): boolean {
	return (
		error instanceof Error && error.name === 'AbortError'
	) || (typeof DOMException !== 'undefined' && error instanceof DOMException && error.name === 'AbortError');
}

export function deriveReviewLoadPhase(snapshot: ReviewLoadSnapshot): ReviewLoadPhase {
	if (!snapshot.hasUsableJob && (snapshot.isPending || snapshot.isFetching)) return 'loading';
	if (!snapshot.hasUsableJob && snapshot.hasError && !snapshot.isFetching) return 'error';
	if (snapshot.hasUsableJob && snapshot.hasError) return 'refresh-error';
	return 'ready';
}
