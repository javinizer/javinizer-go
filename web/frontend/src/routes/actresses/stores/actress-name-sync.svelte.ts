import { apiClient } from '$lib/api/client';
import { ApiError } from '$lib/api/clients/common';
import type { Actress } from '$lib/api/types';
import type {
	ActressNameSyncStatus,
	ActressNameSyncSource,
	ActressNameSyncReview,
	ActressNameSyncJob,
	ActressNameSyncTask,
} from '$lib/api/types/actress-name-sync';
const RECOVERY_KEY = 'javinizer.offline-name-sync.submitted-review';
type Client = typeof apiClient.actressNameSync;
export interface SyncActor {
	id: number;
	name: string;
	japanese_name: string;
}
interface Dependencies {
	client?: Client;
	storage?: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;
	changed?: () => void;
	now?: () => number;
}
/** Read epochs belong to one dialog session. Only explicitly submitted confirmation
 * references are persisted; restoring a reference never submits it automatically. */
export function createActressNameSyncController(deps: Dependencies = {}) {
	const client = deps.client ?? apiClient.actressNameSync,
		now = deps.now ?? Date.now;
	const storage = () => {
		try {
			return deps.storage ?? (typeof sessionStorage === 'undefined' ? undefined : sessionStorage);
		} catch {
			return undefined;
		}
	};
	let isOpen = $state(false),
		step = $state<'choose' | 'review' | 'history' | 'job'>('choose');
	let actors = $state<SyncActor[]>([]),
		activeActorId = $state<number | null>(null),
		choices = $state<Record<number, ActressNameSyncSource>>({});
	let info = $state<ActressNameSyncStatus | null>(null),
		query = $state(''),
		sourceItems = $state<ActressNameSyncSource[]>([]),
		sourceNextOffset = $state<number | null>(null),
		sourceOffset = $state(0),
		searched = $state(false);
	let review = $state<ActressNameSyncReview | null>(null),
		consent = $state(false),
		jobs = $state<ActressNameSyncJob[]>([]),
		jobsNextCursor = $state<string | undefined>(),
		job = $state<ActressNameSyncJob | null>(null),
		tasks = $state<ActressNameSyncTask[]>([]),
		tasksNextOffset = $state<number | null>(null),
		taskOffset = $state(0);
	let busy = $state<'review' | 'confirm' | 'cancel' | 'reload' | null>(null),
		reloadResult = $state<'' | 'changed' | 'unchanged'>(''),
		errorCode = $state(''),
		infoLoading = $state(false),
		sourceLoading = $state(false),
		historyLoading = $state(false),
		jobLoading = $state(false),
		cancelConfirmOpen = $state(false),
		recoveryReviewId = $state<string | null>(null),
		confirmationUncertain = $state(false);
	let sourceBack = $state<number[]>([]),
		jobBack = $state<string[]>([]),
		historyCursor = '';
	let session = 0,
		poll: ReturnType<typeof setTimeout> | undefined;
	const requests = new Map<string, AbortController>();
	function stop(scope: string) {
		requests.get(scope)?.abort();
		requests.delete(scope);
	}
	function stopPoll() {
		if (poll) clearTimeout(poll);
		poll = undefined;
	}
	function invalidate() {
		session++;
		requests.forEach((c) => c.abort());
		requests.clear();
		stopPoll();
		infoLoading = sourceLoading = historyLoading = jobLoading = false;
		busy = null;
	}
	function begin(scope: string) {
		stop(scope);
		const controller = new AbortController();
		requests.set(scope, controller);
		const epoch = session;
		return {
			signal: controller.signal,
			current: () => isOpen && epoch === session && requests.get(scope) === controller,
		};
	}
	function failure(e: unknown) {
		errorCode = e instanceof ApiError ? (e.code ?? 'SYNC_NETWORK') : 'SYNC_NETWORK';
		if (errorCode === 'SYNC_STALE_GENERATION') {
			clearEvidence();
			// Read-only reconciliation never resubmits a review, reload or confirmation.
			void loadInfo();
		}
	}
	function clearEvidence() {
		stop('sources');
		sourceLoading = false;
		sourceItems = [];
		sourceNextOffset = null;
		sourceOffset = 0;
		sourceBack = [];
		searched = false;
		choices = {};
		review = null;
		consent = false;
		if (busy === 'review') {
			stop('write');
			busy = null;
		}
		if (step === 'review') step = 'choose';
		// A submitted nonce and historical jobs are independent of current evidence.
	}
	function adoptInfo(value: ActressNameSyncStatus) {
		const changed =
			!!info &&
			(info.artifact_digest !== value.artifact_digest ||
				info.enabled !== value.enabled ||
				info.available !== value.available ||
				info.max_age_seconds !== value.max_age_seconds);
		if (changed) clearEvidence();
		info = value;
		return changed;
	}
	function readRecovery() {
		try {
			const id = storage()?.getItem(RECOVERY_KEY);
			return id && /^[a-f0-9]{64}$/.test(id) ? id : null;
		} catch {
			return null;
		}
	}
	function saveRecovery(id: string) {
		recoveryReviewId = id;
		try {
			storage()?.setItem(RECOVERY_KEY, id);
		} catch {
			/* In-memory retry remains available. */
		}
	}
	function clearRecovery(id: string) {
		if (recoveryReviewId === id) {
			recoveryReviewId = null;
			confirmationUncertain = false;
		}
		try {
			if (storage()?.getItem(RECOVERY_KEY) === id) storage()?.removeItem(RECOVERY_KEY);
		} catch {
			/* Storage may be unavailable. */
		}
	}
	function resetReview() {
		stop('write');
		busy = null;
		review = null;
		consent = false;
		errorCode = '';
	}
	async function loadInfo() {
		if (!isOpen || busy === 'reload') return;
		const r = begin('info');
		infoLoading = true;
		try {
			const value = await client.status(r.signal);
			if (r.current() && adoptInfo(value)) reloadResult = 'changed';
		} catch (e) {
			if (r.current()) failure(e);
		} finally {
			if (r.current()) infoLoading = false;
		}
	}
	async function reloadEvidence() {
		if (!isOpen || busy || !info?.enabled) return;
		// A reload races neither this dialog's source reads nor its writes. Other
		// clients remain protected by the server's generation preconditions.
		stop('info');
		stop('sources');
		infoLoading = sourceLoading = false;
		const r = begin('reload');
		busy = 'reload';
		errorCode = '';
		reloadResult = '';
		try {
			const value = await client.reload(r.signal);
			if (r.current()) reloadResult = adoptInfo(value) ? 'changed' : 'unchanged';
		} catch (e) {
			if (r.current()) failure(e);
		} finally {
			if (r.current()) busy = null;
		}
	}
	function open(selected: Actress[] = []) {
		invalidate();
		isOpen = true;
		errorCode = '';
		info = null;
		reloadResult = '';
		review = null;
		consent = false;
		choices = {};
		query = '';
		sourceItems = [];
		sourceBack = [];
		sourceOffset = 0;
		sourceNextOffset = null;
		searched = false;
		job = null;
		tasks = [];
		cancelConfirmOpen = false;
		const seen = new Set<number>();
		actors = selected
			.filter(
				(a) =>
					Number.isSafeInteger(a.id) && (a.id ?? 0) > 0 && !seen.has(a.id!) && !!seen.add(a.id!),
			)
			.slice(0, 501)
			.map((a) => ({
				id: a.id!,
				name:
					[a.first_name, a.last_name].filter(Boolean).join(' ') || a.japanese_name || `#${a.id}`,
				japanese_name: a.japanese_name ?? '',
			}));
		activeActorId = actors[0]?.id ?? null;
		recoveryReviewId = recoveryReviewId ?? readRecovery();
		confirmationUncertain = !!recoveryReviewId;
		step = actors.length ? 'choose' : 'history';
		if (actors.length > 500) errorCode = 'SYNC_SELECTION_LIMIT';
		void loadInfo();
		if (!actors.length) void showHistory();
	}
	function close() {
		invalidate();
		isOpen = false;
		review = null;
		consent = false;
		cancelConfirmOpen = false;
	}
	function setQuery(value: string) {
		stop('sources');
		sourceLoading = false;
		query = value;
		sourceItems = [];
		sourceNextOffset = null;
		sourceOffset = 0;
		sourceBack = [];
		searched = false;
	}
	function selectActor(id: number) {
		if (busy || !actors.some((a) => a.id === id)) return;
		activeActorId = id;
		setQuery('');
	}
	async function sourcePage(offset: number) {
		if (!isOpen || !activeActorId || !info?.enabled || !info.available || infoLoading || busy)
			return;
		if (new TextEncoder().encode(query).length > 128) {
			errorCode = 'SYNC_QUERY_LIMIT';
			return;
		}
		const r = begin('sources'),
			q = query;
		sourceLoading = true;
		errorCode = '';
		try {
			const page = await client.sources(
				{ q, offset, limit: 20, artifact_digest: info.artifact_digest },
				r.signal,
			);
			if (r.current()) {
				if (page.artifact_digest !== info?.artifact_digest) {
					failure(new ApiError('Evidence changed', 'SYNC_STALE_GENERATION'));
					return;
				}
				sourceItems = page.items;
				sourceNextOffset = page.next_offset;
				sourceOffset = offset;
				searched = true;
				return true;
			}
		} catch (e) {
			if (r.current()) failure(e);
		} finally {
			if (r.current()) sourceLoading = false;
		}
	}
	function searchSources() {
		sourceBack = [];
		return sourcePage(0);
	}
	async function moreSources() {
		if (sourceNextOffset === null || sourceLoading) return;
		const previous = sourceOffset;
		if (await sourcePage(sourceNextOffset)) sourceBack = [...sourceBack, previous];
	}
	async function previousSources() {
		if (sourceLoading || !sourceBack.length) return;
		if (await sourcePage(sourceBack.at(-1)!)) sourceBack = sourceBack.slice(0, -1);
	}
	function chooseSource(source: ActressNameSyncSource) {
		if (busy || !activeActorId || !sourceItems.some((s) => s.source_key === source.source_key))
			return;
		if (
			Object.entries(choices).some(
				([id, s]) => Number(id) !== activeActorId && s.source_key === source.source_key,
			)
		) {
			errorCode = 'SYNC_DUPLICATE_SOURCE';
			return;
		}
		choices = { ...choices, [activeActorId]: structuredClone($state.snapshot(source)) };
		resetReview();
	}
	function removeActor(id: number) {
		if (busy) return;
		actors = actors.filter((a) => a.id !== id);
		const next = { ...choices };
		delete next[id];
		choices = next;
		if (activeActorId === id) {
			activeActorId = actors[0]?.id ?? null;
			setQuery('');
		}
		resetReview();
	}
	async function requestReview() {
		if (
			!isOpen ||
			busy ||
			recoveryReviewId ||
			!info?.enabled ||
			!info.available ||
			infoLoading ||
			actors.length < 1 ||
			actors.length > 500 ||
			!actors.every((a) => choices[a.id])
		)
			return;
		const r = begin('write');
		busy = 'review';
		errorCode = '';
		consent = false;
		try {
			const value = await client.review(
				actors.map((a) => ({ actress_id: a.id, source_key: choices[a.id].source_key })),
				r.signal,
				info.artifact_digest,
			);
			if (r.current()) {
				if (
					value.artifact_digest !== info?.artifact_digest ||
					value.max_age_seconds !== info.max_age_seconds
				) {
					failure(new ApiError('Evidence changed', 'SYNC_STALE_GENERATION'));
					return;
				}
				review = value;
				step = 'review';
			}
		} catch (e) {
			if (r.current()) failure(e);
		} finally {
			if (r.current()) busy = null;
		}
	}
	function backToChoices() {
		if (busy === 'confirm' || busy === 'cancel' || busy === 'reload' || recoveryReviewId) return;
		stopPoll();
		stop('job');
		stop('history');
		jobLoading = historyLoading = false;
		cancelConfirmOpen = false;
		resetReview();
		step = 'choose';
	}
	function setConsent(value: boolean) {
		if (!busy) consent = value;
	}
	async function sendConfirmation(id: string) {
		if (!isOpen || busy) return;
		const r = begin('write');
		busy = 'confirm';
		errorCode = '';
		saveRecovery(id);
		confirmationUncertain = true;
		try {
			const value = await client.confirm(id, r.signal);
			clearRecovery(id);
			if (r.current()) {
				confirmationUncertain = false;
				review = null;
				consent = false;
				busy = null;
				deps.changed?.();
				await openJob(value.id);
			}
		} catch (e) {
			if (e instanceof ApiError && e.code === 'SYNC_REVIEW_EXPIRED') {
				clearRecovery(id);
				if (r.current()) {
					confirmationUncertain = false;
					// Expiry can also mean another client reloaded the evidence.
					// Do not leave a checked, unusable confirmation on screen.
					clearEvidence();
					void loadInfo();
				}
			}
			if (r.current()) failure(e);
		} finally {
			if (r.current()) busy = null;
		}
	}
	function confirm() {
		if (!review || !consent || busy) return;
		if (now() >= Date.parse(review.expires_at) && !recoveryReviewId) {
			errorCode = 'SYNC_REVIEW_EXPIRED';
			return;
		}
		return sendConfirmation(review.review_id);
	}
	function retryConfirmation() {
		if (recoveryReviewId) return sendConfirmation(recoveryReviewId);
	}
	function dismissRecovery() {
		if (busy) return;
		if (recoveryReviewId) clearRecovery(recoveryReviewId);
		confirmationUncertain = false;
		review = null;
		consent = false;
		step = actors.length ? 'choose' : 'history';
	}
	async function historyPage(cursor: string) {
		stop('sources');
		sourceLoading = false;
		stopPoll();
		stop('job');
		jobLoading = false;
		step = 'history';
		cancelConfirmOpen = false;
		const r = begin('history');
		historyLoading = true;
		errorCode = '';
		try {
			const page = await client.jobs({ cursor, limit: 20 }, r.signal);
			if (r.current()) {
				jobs = page.items;
				jobsNextCursor = page.next_cursor;
				historyCursor = cursor;
				return true;
			}
		} catch (e) {
			if (r.current()) failure(e);
		} finally {
			if (r.current()) historyLoading = false;
		}
	}
	function showHistory() {
		if (!isOpen || busy) return;
		jobBack = [];
		return historyPage('');
	}
	async function moreJobs() {
		if (historyLoading || !jobsNextCursor) return;
		const previous = historyCursor;
		if (await historyPage(jobsNextCursor)) jobBack = [...jobBack, previous];
	}
	async function previousJobs() {
		if (historyLoading || !jobBack.length) return;
		if (await historyPage(jobBack.at(-1)!)) jobBack = jobBack.slice(0, -1);
	}
	async function loadJob(id: string, offset: number) {
		stopPoll();
		const r = begin('job');
		jobLoading = true;
		errorCode = '';
		try {
			// Header first: terminal headers must never freeze receipts read before final commit.
			const value = await client.job(id, r.signal);
			if (!r.current()) return;
			const page = await client.tasks(id, { offset, limit: 50 }, r.signal);
			if (r.current()) {
				job = value;
				tasks = page.items;
				tasksNextOffset = page.next_offset;
				taskOffset = offset;
				deps.changed?.();
				if (['pending', 'running', 'blocked'].includes(value.status))
					poll = setTimeout(() => {
						if (r.current() && step === 'job') void loadJob(id, offset);
					}, 2000);
			}
		} catch (e) {
			if (r.current()) failure(e);
		} finally {
			if (r.current()) jobLoading = false;
		}
	}
	function openJob(id: string) {
		if (!isOpen || busy) return;
		stop('sources');
		sourceLoading = false;
		stop('history');
		historyLoading = false;
		step = 'job';
		job = null;
		tasks = [];
		taskOffset = 0;
		tasksNextOffset = null;
		cancelConfirmOpen = false;
		return loadJob(id, 0);
	}
	function refreshJob() {
		if (job && !busy) return loadJob(job.id, taskOffset);
	}
	function moreTasks() {
		if (job && tasksNextOffset !== null && !jobLoading && !busy)
			return loadJob(job.id, tasksNextOffset);
	}
	function previousTasks() {
		if (job && taskOffset > 0 && !jobLoading && !busy)
			return loadJob(job.id, Math.max(0, taskOffset - 50));
	}
	function requestCancel() {
		if (job && !busy && ['pending', 'running', 'blocked'].includes(job.status))
			cancelConfirmOpen = true;
	}
	function dismissCancel() {
		if (!busy) cancelConfirmOpen = false;
	}
	async function confirmCancel() {
		if (!isOpen || !job || !cancelConfirmOpen || busy) return;
		const id = job.id,
			r = begin('write');
		busy = 'cancel';
		errorCode = '';
		stopPoll();
		stop('job');
		jobLoading = false;
		try {
			await client.cancel(id, r.signal);
			if (r.current()) {
				cancelConfirmOpen = false;
				busy = null;
				await loadJob(id, taskOffset);
			}
		} catch (e) {
			if (r.current()) failure(e);
		} finally {
			if (r.current()) busy = null;
		}
	}
	return {
		get isOpen() {
			return isOpen;
		},
		get step() {
			return step;
		},
		get actors() {
			return actors;
		},
		get activeActorId() {
			return activeActorId;
		},
		get choices() {
			return choices;
		},
		get info() {
			return info;
		},
		get query() {
			return query;
		},
		get sourceItems() {
			return sourceItems;
		},
		get sourceNextOffset() {
			return sourceNextOffset;
		},
		get sourceOffset() {
			return sourceOffset;
		},
		get sourceHasPrevious() {
			return sourceBack.length > 0;
		},
		get searched() {
			return searched;
		},
		get review() {
			return review;
		},
		get consent() {
			return consent;
		},
		get jobs() {
			return jobs;
		},
		get jobsNextCursor() {
			return jobsNextCursor;
		},
		get historyHasPrevious() {
			return jobBack.length > 0;
		},
		get job() {
			return job;
		},
		get tasks() {
			return tasks;
		},
		get tasksNextOffset() {
			return tasksNextOffset;
		},
		get taskOffset() {
			return taskOffset;
		},
		get busy() {
			return busy;
		},
		get reloadResult() {
			return reloadResult;
		},
		get errorCode() {
			return errorCode;
		},
		get infoLoading() {
			return infoLoading;
		},
		get sourceLoading() {
			return sourceLoading;
		},
		get historyLoading() {
			return historyLoading;
		},
		get jobLoading() {
			return jobLoading;
		},
		get loading() {
			return infoLoading || sourceLoading || historyLoading || jobLoading || !!busy;
		},
		get cancelConfirmOpen() {
			return cancelConfirmOpen;
		},
		get recoveryReviewId() {
			return recoveryReviewId;
		},
		get confirmationUncertain() {
			return confirmationUncertain;
		},
		open,
		close,
		loadInfo,
		reloadEvidence,
		selectActor,
		setQuery,
		searchSources,
		moreSources,
		previousSources,
		chooseSource,
		removeActor,
		requestReview,
		backToChoices,
		setConsent,
		confirm,
		retryConfirmation,
		dismissRecovery,
		showHistory,
		moreJobs,
		previousJobs,
		openJob,
		refreshJob,
		moreTasks,
		previousTasks,
		requestCancel,
		dismissCancel,
		confirmCancel,
	};
}
export type ActressNameSyncController = ReturnType<typeof createActressNameSyncController>;
