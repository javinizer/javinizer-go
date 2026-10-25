import { afterEach, describe, expect, it, vi } from 'vitest';
import { createActressNameSyncController } from './actress-name-sync.svelte';
import { ActressNameSyncClient } from '$lib/api/clients/actress-name-sync';
import { ApiError } from '$lib/api/clients/common';
import type { ActressNameSyncJob, ActressNameSyncSource } from '$lib/api/types/actress-name-sync';
const key = 'javinizer.offline-name-sync.submitted-review',
	nonce = 'a'.repeat(64);
const source: ActressNameSyncSource = {
	source_key: 'v1:r18dev:MQ',
	source_id: '1',
	japanese_name: '名前',
	aliases: ['alias'],
	source_url: 'https://example.test/1',
	observed_at: '2026-10-25T00:00:00Z',
};
const actor = { id: 17, first_name: 'Candidate', japanese_name: '旧名' };
const job: ActressNameSyncJob = {
	id: 'job-a',
	snapshot_digest: 'b'.repeat(64),
	created_at: '2026-10-25T00:00:00Z',
	cancelled: false,
	status: 'completed',
	counts: { applied: 1 },
};
function deferred<T>() {
	let resolve!: (v: T) => void, reject!: (e: unknown) => void;
	const promise = new Promise<T>((a, b) => {
		resolve = a;
		reject = b;
	});
	return { promise, resolve, reject };
}
const active: ReturnType<typeof createActressNameSyncController>[] = [];
function setup(initial?: string) {
	const values = new Map<string, string>(initial ? [[key, initial]] : []);
	const storage = {
		getItem: vi.fn((k: string) => values.get(k) ?? null),
		setItem: vi.fn((k: string, v: string) => {
			values.set(k, v);
		}),
		removeItem: vi.fn((k: string) => {
			values.delete(k);
		}),
	};
	const client = new ActressNameSyncClient('http://unused.test');
	const api = {
		status: vi.spyOn(client, 'status').mockResolvedValue({
			enabled: true,
			available: true,
			record_count: 1,
			recovered: false,
			max_age_seconds: 3600,
			restart_required_for_config_changes: true,
		}),
		sources: vi
			.spyOn(client, 'sources')
			.mockResolvedValue({ items: [source], next_offset: null, artifact_digest: 'digest' }),
		review: vi.spyOn(client, 'review').mockResolvedValue({
			review_id: nonce,
			expires_at: '2030-01-01T00:00:00Z',
			artifact_digest: 'digest',
			max_age_seconds: 3600,
			proposals: [{ actress_id: 17, source, before_name: '旧名', after_name: '名前' }],
		}),
		confirm: vi.spyOn(client, 'confirm').mockResolvedValue(job),
		jobs: vi.spyOn(client, 'jobs').mockResolvedValue({ items: [job] }),
		job: vi.spyOn(client, 'job').mockResolvedValue(job),
		tasks: vi.spyOn(client, 'tasks').mockResolvedValue({
			items: [
				{
					id: 1,
					actress_id: 17,
					source_key: source.source_key,
					before_name: '旧名',
					after_name: '名前',
					status: 'applied',
				},
			],
			next_offset: null,
		}),
		cancel: vi
			.spyOn(client, 'cancel')
			.mockResolvedValue({ ...job, status: 'cancelled', cancelled: true }),
	};
	const changed = vi.fn();
	const c = createActressNameSyncController({
		client,
		storage,
		changed,
		now: () => Date.parse('2026-10-25'),
	});
	active.push(c);
	return { c, api, storage, changed };
}
async function open(h: ReturnType<typeof setup>) {
	h.c.open([actor]);
	await Promise.resolve();
	await Promise.resolve();
}
async function choose(h: ReturnType<typeof setup>) {
	await open(h);
	h.c.setQuery('Candidate');
	await h.c.searchSources();
	h.c.chooseSource(source);
}
async function review(h: ReturnType<typeof setup>) {
	await choose(h);
	await h.c.requestReview();
}
afterEach(() => {
	active.splice(0).forEach((c) => c.close());
	vi.useRealTimers();
	vi.restoreAllMocks();
});
describe('offline name sync controller', () => {
	it('snapshots and deduplicates valid selected actors without name inference', () => {
		const h = setup();
		h.c.open([actor, actor, { id: 0 }, { id: 18, first_name: 'Candidate' }]);
		expect(h.c.actors.map((a) => a.id)).toEqual([17, 18]);
		actor.first_name = 'Changed';
		expect(h.c.actors[0].name).toBe('Candidate');
		actor.first_name = 'Candidate';
		expect(h.c.choices).toEqual({});
	});
	it('open, search, choose, history, Back and dismissal never write', async () => {
		const h = setup();
		await choose(h);
		await h.c.showHistory();
		h.c.backToChoices();
		h.c.close();
		await open(h);
		expect(h.api.review).not.toHaveBeenCalled();
		expect(h.api.confirm).not.toHaveBeenCalled();
		expect(h.api.cancel).not.toHaveBeenCalled();
		expect(h.c.choices).toEqual({});
	});
	it('requires review then consent and posts only exact selected identities', async () => {
		const h = setup();
		await choose(h);
		await h.c.confirm();
		expect(h.api.confirm).not.toHaveBeenCalled();
		await h.c.requestReview();
		expect(h.api.review.mock.calls[0][0]).toEqual([
			{ actress_id: 17, source_key: source.source_key },
		]);
		await h.c.confirm();
		expect(h.api.confirm).not.toHaveBeenCalled();
		h.c.setConsent(true);
		await h.c.confirm();
		expect(h.api.confirm).toHaveBeenCalledExactlyOnceWith(nonce, expect.any(AbortSignal));
		expect(h.c.step).toBe('job');
		expect(h.storage.getItem(key)).toBeNull();
	});
	it('locks repeated review and confirmation clicks until the request settles', async () => {
		const h = setup();
		await choose(h);
		const d = deferred<Awaited<ReturnType<typeof h.api.review>>>();
		h.api.review.mockReturnValueOnce(d.promise);
		const p = h.c.requestReview();
		await h.c.requestReview();
		expect(h.api.review).toHaveBeenCalledTimes(1);
		d.resolve({
			review_id: nonce,
			expires_at: '2030-01-01T00:00:00Z',
			artifact_digest: 'x',
			max_age_seconds: 1,
			proposals: [],
		});
		await p;
		h.c.setConsent(true);
		const confirmation = deferred<ActressNameSyncJob>();
		h.api.confirm.mockReturnValueOnce(confirmation.promise);
		const p2 = h.c.confirm();
		await h.c.confirm();
		expect(h.api.confirm).toHaveBeenCalledTimes(1);
		expect(h.storage.getItem(key)).toBe(nonce);
		confirmation.resolve(job);
		await p2;
	});
	it('never implicitly chooses ambiguous names and rejects duplicate or unlisted sources', async () => {
		const h = setup();
		h.c.open([actor, { id: 18, first_name: 'Candidate' }]);
		await Promise.resolve();
		await Promise.resolve();
		await h.c.searchSources();
		h.c.chooseSource({ ...source, source_key: 'unlisted' });
		expect(h.c.choices).toEqual({});
		h.c.chooseSource(source);
		h.c.selectActor(18);
		await h.c.searchSources();
		h.c.chooseSource(source);
		expect(h.c.errorCode).toBe('SYNC_DUPLICATE_SOURCE');
		expect(h.c.choices[18]).toBeUndefined();
	});
	it('guards oversized UTF-8 queries and >500 selections before requests', async () => {
		const h = setup();
		await open(h);
		h.c.setQuery('名'.repeat(43));
		await h.c.searchSources();
		expect(h.api.sources).not.toHaveBeenCalled();
		expect(h.c.errorCode).toBe('SYNC_QUERY_LIMIT');
		h.c.open(Array.from({ length: 501 }, (_, i) => ({ id: i + 1 })));
		await h.c.requestReview();
		expect(h.api.review).not.toHaveBeenCalled();
		expect(h.c.errorCode).toBe('SYNC_SELECTION_LIMIT');
	});
	it.each([
		{ enabled: false, available: true },
		{ enabled: true, available: false },
	])('permits only history when unavailable: %j', async (status) => {
		const h = setup();
		h.api.status.mockResolvedValue({
			...status,
			record_count: 0,
			recovered: false,
			max_age_seconds: 0,
			restart_required_for_config_changes: true,
		});
		await open(h);
		await h.c.searchSources();
		await h.c.requestReview();
		await h.c.showHistory();
		expect(h.api.sources).not.toHaveBeenCalled();
		expect(h.api.review).not.toHaveBeenCalled();
		expect(h.api.jobs).toHaveBeenCalledTimes(1);
	});
	it('fences source responses after query changes even when transport ignores abort', async () => {
		const h = setup();
		await open(h);
		const d = deferred<Awaited<ReturnType<typeof h.api.sources>>>();
		h.api.sources.mockReturnValueOnce(d.promise);
		const p = h.c.searchSources();
		const signal = h.api.sources.mock.calls[0][1]!;
		h.c.setQuery('new');
		expect(signal.aborted).toBe(true);
		d.resolve({ items: [source], next_offset: null, artifact_digest: 'x' });
		await p;
		expect(h.c.sourceItems).toEqual([]);
		expect(h.c.query).toBe('new');
	});
	it('fences old status, source errors and reviews across close/reopen', async () => {
		const h = setup();
		await choose(h);
		const d = deferred<Awaited<ReturnType<typeof h.api.review>>>();
		h.api.review.mockReturnValueOnce(d.promise);
		const p = h.c.requestReview();
		h.c.close();
		await open(h);
		d.resolve({
			review_id: nonce,
			expires_at: '2030-01-01T00:00:00Z',
			artifact_digest: 'x',
			max_age_seconds: 1,
			proposals: [],
		});
		await p;
		expect(h.c.review).toBeNull();
		expect(h.c.step).toBe('choose');
		expect(h.c.busy).toBeNull();
	});
	it('Back aborts a pending review and invalidates consent', async () => {
		const h = setup();
		await choose(h);
		const d = deferred<Awaited<ReturnType<typeof h.api.review>>>();
		h.api.review.mockReturnValueOnce(d.promise);
		const p = h.c.requestReview();
		h.c.backToChoices();
		d.resolve({
			review_id: nonce,
			expires_at: '2030-01-01T00:00:00Z',
			artifact_digest: 'x',
			max_age_seconds: 1,
			proposals: [],
		});
		await p;
		expect(h.c.review).toBeNull();
		expect(h.c.consent).toBe(false);
	});
	it('persists only an explicitly submitted nonce and retries it only on request', async () => {
		const h = setup();
		await review(h);
		expect(h.storage.setItem).not.toHaveBeenCalled();
		h.api.confirm.mockRejectedValueOnce(new TypeError('lost'));
		h.c.setConsent(true);
		await h.c.confirm();
		expect(h.storage.getItem(key)).toBe(nonce);
		expect(h.c.confirmationUncertain).toBe(true);
		h.c.close();
		await open(h);
		expect(h.api.confirm).toHaveBeenCalledTimes(1);
		await h.c.requestReview();
		expect(h.api.review).toHaveBeenCalledTimes(1);
		await h.c.retryConfirmation();
		expect(h.api.confirm).toHaveBeenCalledTimes(2);
		expect(h.api.confirm.mock.calls.every(([id]) => id === nonce)).toBe(true);
	});
	it('restores but never automatically replays a submitted nonce', async () => {
		const h = setup(nonce);
		await open(h);
		expect(h.c.recoveryReviewId).toBe(nonce);
		expect(h.api.confirm).not.toHaveBeenCalled();
		expect(h.api.review).not.toHaveBeenCalled();
		h.c.dismissRecovery();
		expect(h.storage.getItem(key)).toBeNull();
		expect(h.api.cancel).not.toHaveBeenCalled();
	});
	it.each([
		'SYNC_STALE',
		'SYNC_PROTECTED',
		'AUTH_UNAUTHORIZED',
		'SYNC_REVIEW_PRINCIPAL',
		'SYNC_DISABLED',
	])('retains recovery on nondefinitive %s errors', async (code) => {
		const h = setup(nonce);
		h.api.confirm.mockRejectedValue(new ApiError('private', code));
		await open(h);
		await h.c.retryConfirmation();
		expect(h.c.errorCode).toBe(code);
		expect(h.storage.getItem(key)).toBe(nonce);
	});
	it('clears recovery only for definitive expired review', async () => {
		const h = setup(nonce);
		h.api.confirm.mockRejectedValue(new ApiError('expired', 'SYNC_REVIEW_EXPIRED'));
		await open(h);
		await h.c.retryConfirmation();
		expect(h.storage.getItem(key)).toBeNull();
		expect(h.c.recoveryReviewId).toBeNull();
	});
	it('rejects an expired unsubmitted review without persisting it', async () => {
		const h = setup();
		h.api.review.mockResolvedValue({
			review_id: nonce,
			expires_at: '2000-01-01T00:00:00Z',
			artifact_digest: 'x',
			max_age_seconds: 1,
			proposals: [],
		});
		await review(h);
		h.c.setConsent(true);
		await h.c.confirm();
		expect(h.api.confirm).not.toHaveBeenCalled();
		expect(h.storage.setItem).not.toHaveBeenCalled();
		expect(h.c.errorCode).toBe('SYNC_REVIEW_EXPIRED');
	});
	it('ignores malformed stored recovery data', async () => {
		const h = setup('not-a-nonce');
		await open(h);
		expect(h.c.recoveryReviewId).toBeNull();
	});
	it('does not reopen a new session after old confirmation completes', async () => {
		const h = setup();
		await review(h);
		h.c.setConsent(true);
		const d = deferred<ActressNameSyncJob>();
		h.api.confirm.mockReturnValueOnce(d.promise);
		const p = h.c.confirm();
		h.c.close();
		await open(h);
		d.resolve(job);
		await p;
		expect(h.c.step).toBe('choose');
		expect(h.api.job).not.toHaveBeenCalled();
		expect(h.changed).not.toHaveBeenCalled();
	});
	it('reads job header before tasks, avoiding frozen pre-terminal receipts', async () => {
		const h = setup();
		await open(h);
		const d = deferred<ActressNameSyncJob>();
		h.api.job.mockReturnValueOnce(d.promise);
		const p = h.c.openJob('job-a');
		expect(h.api.tasks).not.toHaveBeenCalled();
		d.resolve(job);
		await p;
		expect(h.api.tasks).toHaveBeenCalledExactlyOnceWith(
			'job-a',
			{ offset: 0, limit: 50 },
			expect.any(AbortSignal),
		);
		expect(h.c.tasks[0].status).toBe('applied');
	});
	it('ignores older job results after a newer navigation', async () => {
		const h = setup();
		await open(h);
		const d = deferred<ActressNameSyncJob>();
		h.api.job.mockReturnValueOnce(d.promise);
		const old = h.c.openJob('old');
		await h.c.openJob('new');
		d.resolve({ ...job, id: 'old' });
		await old;
		expect(h.api.tasks).not.toHaveBeenCalledWith('old', expect.anything(), expect.anything());
		expect(h.c.job?.id).toBe('job-a');
	});
	it('requires a separate cancellation confirmation and keeps applied receipts', async () => {
		const h = setup();
		h.api.job.mockResolvedValue({ ...job, status: 'running', counts: { applied: 1, pending: 1 } });
		await open(h);
		await h.c.openJob('job-a');
		await h.c.confirmCancel();
		expect(h.api.cancel).not.toHaveBeenCalled();
		h.c.requestCancel();
		h.c.dismissCancel();
		await h.c.confirmCancel();
		expect(h.api.cancel).not.toHaveBeenCalled();
		h.c.requestCancel();
		h.api.job.mockResolvedValue({
			...job,
			status: 'cancelled',
			cancelled: true,
			counts: { applied: 1, cancelled: 1 },
		});
		await h.c.confirmCancel();
		expect(h.api.cancel).toHaveBeenCalledTimes(1);
		expect(h.c.tasks[0].status).toBe('applied');
		expect(h.c.job?.counts).toEqual({ applied: 1, cancelled: 1 });
	});
	it('closing cancellation warning and completed jobs never cancel', async () => {
		const h = setup();
		await open(h);
		await h.c.openJob('job-a');
		h.c.requestCancel();
		expect(h.c.cancelConfirmOpen).toBe(false);
		h.c.close();
		await h.c.confirmCancel();
		expect(h.api.cancel).not.toHaveBeenCalled();
	});
	it('polls active jobs only while viewed and stops after dismissal', async () => {
		vi.useFakeTimers();
		const h = setup();
		h.api.job.mockResolvedValue({ ...job, status: 'running' });
		await open(h);
		await h.c.openJob('job-a');
		await vi.advanceTimersByTimeAsync(2001);
		expect(h.api.job).toHaveBeenCalledTimes(2);
		h.c.close();
		await vi.advanceTimersByTimeAsync(10000);
		expect(h.api.job).toHaveBeenCalledTimes(2);
	});
	it('replaces source pages and preserves Back history on failed pagination', async () => {
		const h = setup();
		await open(h);
		h.api.sources.mockResolvedValueOnce({ items: [source], next_offset: 20, artifact_digest: 'x' });
		await h.c.searchSources();
		h.api.sources.mockRejectedValueOnce(new Error('lost'));
		await h.c.moreSources();
		expect(h.c.sourceHasPrevious).toBe(false);
		h.api.sources.mockResolvedValueOnce({
			items: [{ ...source, source_id: '2' }],
			next_offset: null,
			artifact_digest: 'x',
		});
		await h.c.moreSources();
		expect(h.c.sourceHasPrevious).toBe(true);
		expect(h.c.sourceItems).toHaveLength(1);
		expect(h.api.sources.mock.lastCall?.[0]).toMatchObject({ offset: 20, limit: 20 });
		await h.c.previousSources();
		expect(h.api.sources.mock.lastCall?.[0]).toMatchObject({ offset: 0 });
	});
	it('uses opaque history cursors and bounded replacement task pages', async () => {
		const h = setup();
		await open(h);
		h.api.jobs.mockResolvedValueOnce({ items: [job], next_cursor: 'opaque/+=' });
		await h.c.showHistory();
		await h.c.moreJobs();
		expect(h.api.jobs.mock.lastCall?.[0]).toEqual({ cursor: 'opaque/+=', limit: 20 });
		expect(h.c.historyHasPrevious).toBe(true);
		h.api.tasks.mockResolvedValueOnce({ items: [], next_offset: 50 });
		await h.c.openJob('job-a');
		await h.c.moreTasks();
		expect(h.api.tasks.mock.lastCall?.[1]).toEqual({ offset: 50, limit: 50 });
		expect(h.c.tasks).toHaveLength(1);
	});
	it('retains in-memory submitted recovery when browser storage is blocked', async () => {
		const h = setup();
		h.storage.setItem.mockImplementation(() => {
			throw new Error('blocked');
		});
		h.storage.getItem.mockImplementation(() => {
			throw new Error('blocked');
		});
		await review(h);
		h.api.confirm.mockRejectedValueOnce(new TypeError('lost'));
		h.c.setConsent(true);
		await h.c.confirm();
		expect(h.c.recoveryReviewId).toBe(nonce);
		h.c.close();
		await open(h);
		expect(h.c.recoveryReviewId).toBe(nonce);
		await h.c.retryConfirmation();
		expect(h.api.confirm).toHaveBeenCalledTimes(2);
		expect(h.c.recoveryReviewId).toBeNull();
	});
	it('fences old status/error reads after close and a newer session', async () => {
		const h = setup();
		const d = deferred<Awaited<ReturnType<typeof h.api.status>>>();
		h.api.status.mockReturnValueOnce(d.promise);
		h.c.open([actor]);
		h.c.close();
		await open(h);
		d.reject(new ApiError('old', 'SYNC_DISABLED'));
		await Promise.resolve();
		expect(h.c.info?.enabled).toBe(true);
		expect(h.c.errorCode).toBe('');
	});
	it('fences a pending source error after active actress changes', async () => {
		const h = setup();
		h.c.open([actor, { id: 18 }]);
		await Promise.resolve();
		await Promise.resolve();
		const d = deferred<Awaited<ReturnType<typeof h.api.sources>>>();
		h.api.sources.mockReturnValueOnce(d.promise);
		const p = h.c.searchSources();
		h.c.selectActor(18);
		d.reject(new Error('old'));
		await p;
		expect(h.c.errorCode).toBe('');
		expect(h.c.sourceLoading).toBe(false);
	});
	it('fences old receipt pages after switching from a job to history', async () => {
		const h = setup();
		await open(h);
		const d = deferred<Awaited<ReturnType<typeof h.api.tasks>>>();
		h.api.tasks.mockReturnValueOnce(d.promise);
		const p = h.c.openJob('old');
		await Promise.resolve();
		await Promise.resolve();
		await h.c.showHistory();
		d.resolve({ items: [], next_offset: null });
		await p;
		expect(h.c.step).toBe('history');
		expect(h.c.job).toBeNull();
	});
	it('fences history responses after opening a newer job', async () => {
		const h = setup();
		await open(h);
		const d = deferred<Awaited<ReturnType<typeof h.api.jobs>>>();
		h.api.jobs.mockReturnValueOnce(d.promise);
		const p = h.c.showHistory();
		await h.c.openJob('job-a');
		d.resolve({ items: [], next_cursor: 'old' });
		await p;
		expect(h.c.step).toBe('job');
		expect(h.c.jobsNextCursor).toBeUndefined();
	});
	it('prevents repeated cancellation and preserves receipts on failure', async () => {
		const h = setup();
		h.api.job.mockResolvedValue({ ...job, status: 'running' });
		await open(h);
		await h.c.openJob('job-a');
		h.c.requestCancel();
		const d = deferred<ActressNameSyncJob>();
		h.api.cancel.mockReturnValueOnce(d.promise);
		const p = h.c.confirmCancel();
		await h.c.confirmCancel();
		expect(h.api.cancel).toHaveBeenCalledTimes(1);
		d.reject(new ApiError('failed', 'SYNC_CONFLICT'));
		await p;
		expect(h.c.tasks[0].status).toBe('applied');
		expect(h.c.errorCode).toBe('SYNC_CONFLICT');
		expect(h.c.cancelConfirmOpen).toBe(true);
		expect(h.c.busy).toBeNull();
	});
	it('keeps history paging cursor and Back stack intact after failure', async () => {
		const h = setup();
		await open(h);
		h.api.jobs.mockResolvedValueOnce({ items: [job], next_cursor: 'next' });
		await h.c.showHistory();
		h.api.jobs.mockRejectedValueOnce(new Error('lost'));
		await h.c.moreJobs();
		expect(h.c.historyHasPrevious).toBe(false);
		expect(h.c.jobsNextCursor).toBe('next');
		await h.c.moreJobs();
		expect(h.c.historyHasPrevious).toBe(true);
		await h.c.previousJobs();
		expect(h.api.jobs.mock.lastCall?.[0]).toEqual({ cursor: '', limit: 20 });
	});
	it('locks task page navigation while a prior page is pending', async () => {
		const h = setup();
		await open(h);
		h.api.tasks.mockResolvedValueOnce({ items: [], next_offset: 50 });
		await h.c.openJob('job-a');
		const d = deferred<Awaited<ReturnType<typeof h.api.tasks>>>();
		h.api.tasks.mockReturnValueOnce(d.promise);
		const p = h.c.moreTasks();
		await h.c.moreTasks();
		await Promise.resolve();
		await Promise.resolve();
		expect(h.api.tasks).toHaveBeenCalledTimes(2);
		d.resolve({ items: [], next_offset: null });
		await p;
		expect(h.c.taskOffset).toBe(50);
		await h.c.moreTasks();
		expect(h.api.tasks).toHaveBeenCalledTimes(2);
	});
});
