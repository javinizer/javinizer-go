import { describe, it, expect, beforeEach } from 'vitest';
import {
	getBackgroundJobState,
	startJob,
	closeModal,
	reopenModal,
	dismiss,
	restoreJob,
} from './background-job.svelte';

beforeEach(() => {
	dismiss();
});

describe('background-job store', () => {
	it('startJob tracks the job and opens the modal', () => {
		startJob('a');
		expect(getBackgroundJobState()).toEqual({ jobId: 'a', showModal: true });
	});

	it('restoreJob tracks a running job without reopening the modal', () => {
		restoreJob('b');
		expect(getBackgroundJobState()).toEqual({ jobId: 'b', showModal: false });
	});

	it('restoreJob is a no-op when a job is already tracked', () => {
		startJob('in-flight');
		restoreJob('late-restore');
		expect(getBackgroundJobState()).toEqual({ jobId: 'in-flight', showModal: true });
	});

	it('restoreJob works after dismiss clears the previous job', () => {
		startJob('gone');
		dismiss();
		restoreJob('restored');
		expect(getBackgroundJobState()).toEqual({ jobId: 'restored', showModal: false });
	});

	it('closeModal keeps the job tracked for the indicator; reopenModal brings the modal back', () => {
		startJob('c');
		closeModal();
		expect(getBackgroundJobState()).toEqual({ jobId: 'c', showModal: false });
		reopenModal();
		expect(getBackgroundJobState()).toEqual({ jobId: 'c', showModal: true });
	});
});
