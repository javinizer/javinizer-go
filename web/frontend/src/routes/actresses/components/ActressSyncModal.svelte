<script lang="ts">
	import { CircleCheck, Loader2, X } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import type { ActressSyncJob, ActressSyncTask } from '$lib/api/types';
	import { buildActressSyncSummary } from '../sync-runner';
	import * as m from '$lib/paraglide/messages';

	let { job, tasks, onCancel, onClose }: { job: ActressSyncJob; tasks: ActressSyncTask[]; onCancel: () => void; onClose: () => void } = $props();
	let summary = $derived(buildActressSyncSummary(job, tasks));
	let progress = $derived(summary.total ? Math.round(summary.processed * 100 / summary.total) : 100);
	let terminal = $derived(job.status === 'completed' || job.status === 'cancelled');

	function diagnostic(value: string): string {
		if (value === 'missing_dmm_id') return m.actresses_sync_diag_missing_dmm();
		if (value === 'no_verified_metadata') return m.actresses_sync_diag_no_metadata();
		if (value === 'already_complete') return m.actresses_sync_diag_already_complete();
		if (value === 'verified_no_changes') return m.actresses_sync_diag_verified_no_changes();
		if (value === 'duplicate_active_task') return m.actresses_sync_diag_duplicate();
		if (value === 'attempt_cap_reached') return m.actresses_sync_diag_attempt_cap();
		if (value === 'conflicting_metadata') return m.actresses_sync_diag_conflicting_metadata();
		if (value === 'partial_metadata') return m.actresses_sync_diag_partial_metadata();
		return value;
	}
</script>

<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" role="presentation">
	<Card class="flex max-h-[88vh] w-full max-w-3xl flex-col overflow-hidden" role="dialog" aria-modal="true" aria-labelledby="actress-sync-title">
		<div class="flex items-center justify-between border-b p-5">
			<h2 id="actress-sync-title" class="text-xl font-semibold">{m.actresses_sync_title()}</h2>
			<Button variant="ghost" size="icon" onclick={onClose} aria-label={m.common_close()}><X class="h-4 w-4" /></Button>
		</div>
		<div class="flex-1 space-y-5 overflow-y-auto p-5">
			{#if terminal}
				<div class="flex items-center gap-3 rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-4" role="status">
					<CircleCheck class="h-5 w-5 shrink-0 text-emerald-600" />
					<div><div class="font-medium">{m.actresses_sync_finished()}</div><div class="text-sm text-muted-foreground">{summary.processed} / {summary.total}</div></div>
				</div>
			{:else}
				<div class="space-y-2">
					<div class="flex justify-between text-sm"><span>{m.actresses_sync_progress()}</span><span>{summary.processed} / {summary.total}</span></div>
					<div class="h-2 overflow-hidden rounded bg-secondary"><div class="h-full bg-primary" style={`width:${progress}%`}></div></div>
				</div>
			{/if}
			<div class="grid grid-cols-3 gap-2 sm:grid-cols-6">
				{#each [[m.actresses_sync_updated(), summary.updated], [m.actresses_sync_warnings(), summary.warnings], [m.actresses_sync_skipped(), summary.skipped], [m.actresses_sync_conflicts(), summary.conflicts], [m.actresses_sync_failed(), summary.failed], [m.actresses_sync_cancelled(), summary.cancelled]] as stat}
					<div class="rounded border p-3"><div class="text-xs text-muted-foreground">{stat[0]}</div><div class="text-xl font-semibold">{stat[1]}</div></div>
				{/each}
			</div>
			{#if summary.active.length}
				<section><h3 class="mb-2 text-sm font-medium">{m.actresses_sync_active()}</h3>{#each summary.active as task (task.id)}<div class="mb-2 flex items-center gap-2 rounded border p-2"><Loader2 class="h-4 w-4 animate-spin" />{task.label} · {task.stage}</div>{/each}</section>
			{/if}
			{#if summary.diagnostics.length}
				<section><h3 class="mb-2 text-sm font-medium">{m.actresses_sync_diagnostics()}</h3>{#each summary.diagnostics as task (task.id)}<div class="mb-2 rounded border p-2 text-sm"><strong>{task.label}</strong><div class="text-muted-foreground">{diagnostic(task.error_message || task.warning || task.messages.join(', '))}</div></div>{/each}</section>
			{/if}
		</div>
		<div class="flex justify-end gap-2 border-t p-4">
			{#if job.status === 'pending' || job.status === 'running'}<Button variant="outline" onclick={onCancel} disabled={job.cancel_requested}>{m.actresses_sync_cancel()}</Button>{/if}
			<Button onclick={onClose}>{m.common_close()}</Button>
		</div>
	</Card>
</div>