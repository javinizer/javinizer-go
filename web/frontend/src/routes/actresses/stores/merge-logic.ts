export function isRetryableActressMergeError(error: unknown): boolean {
	return (
		typeof error === 'object' &&
		error !== null &&
		'code' in error &&
		(error as { code?: unknown }).code === 'ACTRESS_MERGE_STALE_PLAN'
	);
}
