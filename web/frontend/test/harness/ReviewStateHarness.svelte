<script lang="ts">
	import { untrack } from 'svelte';
	import { createReviewState } from '../../src/routes/review/[jobId]/stores/review-state.svelte';
	let {
		jobId,
		capture,
		fromJobs = false,
	}: {
		jobId: string;
		fromJobs?: boolean;
		capture: (value: ReturnType<typeof createReviewState>) => void;
	} = $props();
	const state = createReviewState(() => jobId, () => fromJobs);
	untrack(() => capture(state));
</script>
<div>{state.job?.id ?? 'loading'}</div>
