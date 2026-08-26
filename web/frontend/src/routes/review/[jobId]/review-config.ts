export const REVIEW_DETAIL_TIMEOUT_MS = 30_000;

function parseDevelopmentTimeout(value: unknown): number | undefined {
	if (typeof value !== 'string' || !/^\d+$/.test(value)) return undefined;
	const parsed = Number(value);
	return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

export function getReviewDetailTimeoutMs(): number {
	if (import.meta.env.DEV) {
		return parseDevelopmentTimeout(import.meta.env.VITE_REVIEW_DETAIL_TIMEOUT_MS) ?? REVIEW_DETAIL_TIMEOUT_MS;
	}
	return REVIEW_DETAIL_TIMEOUT_MS;
}
