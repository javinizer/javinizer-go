<script lang="ts">
 import { tick } from 'svelte';
 import type { ActressNameSyncController } from '../stores/actress-name-sync.svelte';
 import Button from '$lib/components/ui/Button.svelte';
 import * as m from '$lib/paraglide/messages';
 let { controller: c }: { controller: ActressNameSyncController } = $props();
 let cancelHeading = $state<HTMLHeadingElement>();
 let cancelTrigger = $state<HTMLSpanElement>();
 let hadCancel = false;
 $effect(() => {
  const open = c.cancelConfirmOpen;
  if (open) { hadCancel = true; void tick().then(() => { if(c.cancelConfirmOpen) cancelHeading?.focus(); }); }
  else if (hadCancel) { hadCancel = false; void tick().then(() => { if(!c.cancelConfirmOpen) cancelTrigger?.querySelector<HTMLButtonElement>('button')?.focus(); }); }
 });
 function retryTime(value: number | undefined): string {
  if (!value || !Number.isFinite(value)) return '—';
  const date = new Date(value); return Number.isNaN(date.getTime()) ? '—' : date.toISOString();
 }
 function status(s: string) { switch(s) {
 case 'pending':return m.actress_sync_pending(); case 'running':return m.actress_sync_running(); case 'blocked':return m.actress_sync_blocked(); case 'completed':return m.actress_sync_completed(); case 'cancelled':return m.actress_sync_cancelled(); case 'applied':return m.actress_sync_applied(); case 'unchanged':return m.actress_sync_unchanged(); case 'skipped':return m.actress_sync_skipped(); case 'invalidated':return m.actress_sync_invalidated(); case 'leased':case 'applying':return m.actress_sync_running(); default:return m.actress_sync_unknown();
 }}
</script>
{#if c.step === 'history'}
 <section class="stack">
  <h3 data-sync-step tabindex="-1">{m.actress_sync_history_title()}</h3>
  <p>{m.actress_sync_history_help()}</p><Button variant="outline" onclick={c.showHistory} disabled={c.historyLoading || !!c.busy}>{m.actress_sync_refresh_history()}</Button>
  {#if c.historyLoading}<p role="status">{m.actress_sync_loading()}</p>{/if}
  {#if !c.historyLoading && !c.jobs.length}<p>{m.actress_sync_no_jobs()}</p>{/if}
  <ul class="stack">{#each c.jobs as job (job.id)}
   <li class="receipt"><p class="break">{job.id}</p><p>{job.created_at} · {status(job.status)}</p><Button onclick={() => c.openJob(job.id)} disabled={!!c.busy}>{m.actress_sync_view_results()}</Button></li>
  {/each}</ul>
  <div class="actions"><Button variant="outline" onclick={c.previousJobs} disabled={!c.historyHasPrevious || c.historyLoading}>{m.actress_sync_previous()}</Button><Button variant="outline" onclick={c.moreJobs} disabled={!c.jobsNextCursor || c.historyLoading}>{m.actress_sync_next()}</Button></div>
 </section>
{:else if c.step === 'job'}
 <section class="stack">
  <h3 data-sync-step tabindex="-1">{m.actress_sync_results()}</h3>
  {#if c.jobLoading}<p role="status">{m.actress_sync_loading()}</p>{/if}
  {#if c.job}
   <p role="status">{status(c.job.status)}</p><p class="break">{c.job.id}</p><p class="break">{m.actress_sync_artifact()}: {c.job.snapshot_digest}</p>
   <p>{m.actress_sync_completion_help()}</p>
   <dl class="counts">{#each Object.entries(c.job.counts) as [key, value]}<dt>{status(key)}</dt><dd>{value ?? 0}</dd>{/each}</dl>
   <div class="actions"><Button variant="outline" onclick={c.refreshJob} disabled={c.jobLoading || !!c.busy}>{m.actress_sync_refresh()}</Button>
    {#if ['pending','running','blocked'].includes(c.job.status)}<span bind:this={cancelTrigger}><Button variant="outline" onclick={c.requestCancel} disabled={!!c.busy}>{m.actress_sync_cancel_job()}</Button></span>{/if}
   </div>
   {#if c.cancelConfirmOpen}<aside class="receipt stack"><h4 bind:this={cancelHeading} tabindex="-1">{m.actress_sync_confirm_cancel()}</h4><p role="alert">{m.actress_sync_cancel_warning()}</p><div class="actions"><Button variant="outline" onclick={c.dismissCancel} disabled={!!c.busy}>{m.actress_sync_keep_job()}</Button><Button variant="destructive" onclick={c.confirmCancel} disabled={!!c.busy}>{m.actress_sync_confirm_cancel()}</Button></div></aside>{/if}
   <ul class="stack">{#each c.tasks as task (task.id)}<li class="receipt stack">
    <p>{m.actress_sync_actor_id({id:task.actress_id})} · {status(task.status)}</p>
    <p class="break">{task.source_key}</p>
    <p>{m.actress_sync_before()}: <span lang="ja">{task.before_name || m.actress_sync_empty_name()}</span></p>
    <p>{m.actress_sync_after()}: <span lang="ja">{task.after_name || m.actress_sync_empty_name()}</span></p>
    {#if task.reason}<p>{m.actress_sync_reason()}: <code>{task.reason}</code></p>{/if}
    {#if task.problem_code}<p>{m.actress_sync_operational_problem()}: <code>{task.problem_code}</code> · {task.problem_attempts ?? 0} · {retryTime(task.retry_after_ms)}</p>{/if}
    {#if task.finished_at}<p>{task.finished_at}</p>{/if}
   </li>{/each}</ul>
   <div class="actions"><Button variant="outline" onclick={c.previousTasks} disabled={!c.taskOffset || c.jobLoading || !!c.busy}>{m.actress_sync_previous()}</Button><Button variant="outline" onclick={c.moreTasks} disabled={c.tasksNextOffset === null || c.jobLoading || !!c.busy}>{m.actress_sync_next()}</Button></div>
  {/if}
 </section>
{/if}
<style>
 .stack{display:grid;gap:.75rem;min-width:0} .actions{display:flex;gap:.5rem;flex-wrap:wrap}.receipt{padding:.75rem;border:1px solid hsl(var(--border));border-radius:.5rem;overflow-wrap:anywhere}.break{overflow-wrap:anywhere}ul{list-style:none;padding:0}.counts{display:grid;grid-template-columns:1fr auto;gap:.25rem}dd{margin:0}h3{font-weight:600;font-size:1.1rem}
</style>
