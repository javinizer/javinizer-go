interface BackgroundJobState {
	jobId: string | null;
	showModal: boolean;
}

let state = $state<BackgroundJobState>({
	jobId: null,
	showModal: false,
});

export function getBackgroundJobState(): BackgroundJobState {
	return state;
}

export function startJob(jobId: string) {
	state.jobId = jobId;
	state.showModal = true;
}

export function closeModal() {
	state.showModal = false;
}

export function reopenModal() {
	if (state.jobId) state.showModal = true;
}

export function dismiss() {
	state.jobId = null;
	state.showModal = false;
}

// restoreJob re-tracks an in-flight job without reopening the modal — used
// after a full page load where the in-memory state is lost and the layout
// re-discovers a running job from the API. startJob stays the entry point
// for user-initiated scrapes (opens the modal immediately).
export function restoreJob(jobId: string) {
	if (state.jobId) return;
	state.jobId = jobId;
	state.showModal = false;
}
