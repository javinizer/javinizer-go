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
