import { describe, expect, it, vi, afterEach } from 'vitest';
import {
	deriveReviewLoadPhase,
	isAbortError,
	validateReviewJobResponse,
} from './review-load-state';
import { getReviewDetailTimeoutMs, REVIEW_DETAIL_TIMEOUT_MS } from '../review-config';
import type { BatchJobResponse } from '$lib/api/types';

describe('validateReviewJobResponse', () => {
	it('rejects empty and mismatched successful responses', () => {
		expect(() => validateReviewJobResponse(undefined, 'job-1')).toThrow(
			'Review job response was empty or for a different job',
		);
		expect(() => validateReviewJobResponse({ id: 'job-2' } as BatchJobResponse, 'job-1')).toThrow();
	});
});

describe('deriveReviewLoadPhase', () => {
	it('loads while the initial request or retry is fetching without a job', () => {
		expect(
			deriveReviewLoadPhase({
				hasUsableJob: false,
				isPending: true,
				isFetching: true,
				hasError: false,
			}),
		).toBe('loading');
		expect(
			deriveReviewLoadPhase({
				hasUsableJob: false,
				isPending: false,
				isFetching: true,
				hasError: true,
			}),
		).toBe('loading');
	});

	it('shows a blocking error after an initial request settles with no job', () => {
		expect(
			deriveReviewLoadPhase({
				hasUsableJob: false,
				isPending: false,
				isFetching: false,
				hasError: true,
			}),
		).toBe('error');
	});

	it('keeps usable content with a refresh error while fetching or settled', () => {
		expect(
			deriveReviewLoadPhase({
				hasUsableJob: true,
				isPending: false,
				isFetching: true,
				hasError: true,
			}),
		).toBe('refresh-error');
		expect(
			deriveReviewLoadPhase({
				hasUsableJob: true,
				isPending: false,
				isFetching: false,
				hasError: true,
			}),
		).toBe('refresh-error');
	});

	it('returns ready for successful usable data', () => {
		expect(
			deriveReviewLoadPhase({
				hasUsableJob: true,
				isPending: false,
				isFetching: false,
				hasError: false,
			}),
		).toBe('ready');
	});

	it('recognizes abort errors without treating other errors as cancellation', () => {
		expect(isAbortError(new DOMException('aborted', 'AbortError'))).toBe(true);
		expect(isAbortError(new Error('network failed'))).toBe(false);
	});
});

describe('getReviewDetailTimeoutMs', () => {
	afterEach(() => vi.unstubAllEnvs());

	it('uses the named default outside development', () => {
		vi.stubEnv('DEV', false);
		vi.stubEnv('VITE_REVIEW_DETAIL_TIMEOUT_MS', '5');
		expect(getReviewDetailTimeoutMs()).toBe(REVIEW_DETAIL_TIMEOUT_MS);
	});

	it('accepts only positive integer development overrides', () => {
		vi.stubEnv('DEV', true);
		vi.stubEnv('VITE_REVIEW_DETAIL_TIMEOUT_MS', '50');
		expect(getReviewDetailTimeoutMs()).toBe(50);
		vi.stubEnv('VITE_REVIEW_DETAIL_TIMEOUT_MS', '0');
		expect(getReviewDetailTimeoutMs()).toBe(REVIEW_DETAIL_TIMEOUT_MS);
		vi.stubEnv('VITE_REVIEW_DETAIL_TIMEOUT_MS', '50.5');
		expect(getReviewDetailTimeoutMs()).toBe(REVIEW_DETAIL_TIMEOUT_MS);
	});
});
