import { BaseClient } from './common';
import {
	ACTRESS_NAME_SYNC_MAX_JOB_PAGE_SIZE,
	ACTRESS_NAME_SYNC_MAX_PAGE_SIZE,
	type ActressNameSyncChoice,
	type ActressNameSyncJob,
	type ActressNameSyncJobPage,
	type ActressNameSyncJobsParams,
	type ActressNameSyncPageParams,
	type ActressNameSyncReview,
	type ActressNameSyncSourcePage,
	type ActressNameSyncSourcesParams,
	type ActressNameSyncStatus,
	type ActressNameSyncTaskPage,
} from '../types/actress-name-sync';
const prefix = '/api/v1/actress-name-sync';
const writeHeaders = { 'X-Requested-With': 'javinizer-offline-name-sync' };
function addPageNumber(
	query: URLSearchParams,
	key: 'offset' | 'limit',
	value: number | undefined,
	maximum = Number.MAX_SAFE_INTEGER,
) {
	if (value === undefined) return;
	if (!Number.isSafeInteger(value) || value < (key === 'limit' ? 1 : 0) || value > maximum)
		throw new RangeError(`Invalid offline name sync ${key}`);
	query.set(key, String(value));
}
function pageQuery(params?: ActressNameSyncPageParams) {
	const q = new URLSearchParams();
	addPageNumber(q, 'offset', params?.offset);
	addPageNumber(q, 'limit', params?.limit, ACTRESS_NAME_SYNC_MAX_PAGE_SIZE);
	return q;
}
function suffix(q: URLSearchParams) {
	return q.size ? `?${q}` : '';
}
/** Mutations are explicit and never retried automatically. */
export class ActressNameSyncClient extends BaseClient {
	async status(signal?: AbortSignal): Promise<ActressNameSyncStatus> {
		return this.request(`${prefix}/status`, { signal });
	}
	async sources(
		params?: ActressNameSyncSourcesParams,
		signal?: AbortSignal,
	): Promise<ActressNameSyncSourcePage> {
		const q = pageQuery(params);
		if (params?.q !== undefined) q.set('q', params.q);
		return this.request(`${prefix}/sources${suffix(q)}`, { signal });
	}
	async review(
		choices: readonly ActressNameSyncChoice[],
		signal?: AbortSignal,
	): Promise<ActressNameSyncReview> {
		return this.request(`${prefix}/reviews`, {
			method: 'POST',
			headers: writeHeaders,
			body: JSON.stringify({
				choices: choices.map(({ actress_id, source_key }) => ({ actress_id, source_key })),
			}),
			signal,
		});
	}
	async confirm(reviewId: string, signal?: AbortSignal): Promise<ActressNameSyncJob> {
		return this.request(`${prefix}/jobs`, {
			method: 'POST',
			headers: writeHeaders,
			body: JSON.stringify({ review_id: reviewId, confirmed: true }),
			signal,
		});
	}
	async jobs(
		params?: ActressNameSyncJobsParams,
		signal?: AbortSignal,
	): Promise<ActressNameSyncJobPage> {
		const q = new URLSearchParams();
		if (params?.cursor !== undefined) q.set('cursor', params.cursor);
		addPageNumber(q, 'limit', params?.limit, ACTRESS_NAME_SYNC_MAX_JOB_PAGE_SIZE);
		return this.request(`${prefix}/jobs${suffix(q)}`, { signal });
	}
	async job(id: string, signal?: AbortSignal): Promise<ActressNameSyncJob> {
		return this.request(`${prefix}/jobs/${encodeURIComponent(id)}`, { signal });
	}
	async tasks(
		id: string,
		params?: ActressNameSyncPageParams,
		signal?: AbortSignal,
	): Promise<ActressNameSyncTaskPage> {
		return this.request(
			`${prefix}/jobs/${encodeURIComponent(id)}/tasks${suffix(pageQuery(params))}`,
			{ signal },
		);
	}
	async cancel(id: string, signal?: AbortSignal): Promise<ActressNameSyncJob> {
		return this.request(`${prefix}/jobs/${encodeURIComponent(id)}/cancel`, {
			method: 'POST',
			headers: writeHeaders,
			body: JSON.stringify({ confirmed: true }),
			signal,
		});
	}
}
