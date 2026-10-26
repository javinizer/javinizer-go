<script lang="ts">
 import { onDestroy, tick } from 'svelte';
 import Button from '$lib/components/ui/Button.svelte';
 import type { ActressNameSyncController } from '../stores/actress-name-sync.svelte';
 import ActressNameSyncEvidence from './ActressNameSyncEvidence.svelte';
 import ActressNameSyncHistory from './ActressNameSyncHistory.svelte';
 import * as m from '$lib/paraglide/messages';
 let { controller: c }: { controller: ActressNameSyncController } = $props();
 let dialog = $state<HTMLDialogElement>();
 let opener: HTMLElement | null = null;
 let forget = $state(false);
 let now = $state(Date.now());
 let available = $derived(!!c.info?.enabled && !!c.info?.available && !c.infoLoading);
 let locked = $derived(!!c.busy || !!c.recoveryReviewId);
 let activeActor = $derived(c.actors.find(a => a.id === c.activeActorId));
 let expired = $derived(!!c.review && now >= Date.parse(c.review.expires_at));
 let canReview = $derived(available && !locked && c.actors.length > 0 && c.actors.length <= 500 && c.actors.every(a => !!c.choices[a.id]));
 // Native modal dialogs can tab into browser chrome; keep keyboard navigation
 // within the currently visible, enabled controls, including dynamic receipts.
 function containTab(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !dialog?.open) return;
  const controls = Array.from(dialog.querySelectorAll<HTMLElement>('a[href], button, input, select, textarea, [tabindex]'))
   .filter(el => el.tabIndex >= 0 && !el.matches(':disabled') && !el.closest('[hidden], [inert], [aria-hidden="true"]') && el.getClientRects().length > 0);
  const first = controls[0]; const last = controls[controls.length - 1];
  const active = document.activeElement;
  if (!first) { event.preventDefault(); dialog.querySelector<HTMLElement>('[data-sync-title]')?.focus(); }
  else if (event.shiftKey && (active === first || !controls.includes(active as HTMLElement))) { event.preventDefault(); last.focus(); }
  else if (!event.shiftKey && (active === last || !controls.includes(active as HTMLElement))) { event.preventDefault(); first.focus(); }
 }
 function errorLabel(code:string) {
  switch(code) {
   case 'SYNC_DISABLED':return m.actress_sync_disabled();
   case 'SYNC_UNAVAILABLE':return m.actress_sync_unavailable();
   case 'SYNC_REVIEW_EXPIRED':return m.actress_sync_expired();
   case 'SYNC_REVIEW_PRINCIPAL':return m.actress_sync_error_principal();
   case 'SYNC_PROTECTED':return m.actress_sync_error_protected();
   case 'SYNC_STALE':return m.actress_sync_error_stale();
   case 'SYNC_STALE_GENERATION':return m.actress_sync_reload_changed();
   case 'SYNC_RELOAD_BUSY':return m.actress_sync_reload_busy();
   case 'SYNC_RELOAD_FAILED':return m.actress_sync_reload_failed();
   case 'SYNC_CONFLICT':return m.actress_sync_error_conflict();
   case 'SYNC_NOT_FOUND':return m.actress_sync_error_not_found();
   case 'SYNC_CAPACITY':return m.actress_sync_error_capacity();
   case 'SYNC_SELECTION_LIMIT':return m.actress_sync_selection_limit();
   case 'SYNC_QUERY_LIMIT':return m.actress_sync_query_limit();
   case 'SYNC_DUPLICATE_SOURCE':return m.actress_sync_duplicate_source();
   case 'AUTH_UNAUTHORIZED':case 'SYNC_UNAUTHORIZED':case 'SYNC_FORBIDDEN':return m.actress_sync_error_auth();
   case 'SYNC_WRITE_INTENT_REQUIRED':case 'SYNC_ORIGIN_REJECTED':case 'SYNC_JSON_REQUIRED':return m.actress_sync_error_security();
   case 'SYNC_NETWORK':case 'SYNC_TIMEOUT':case 'SYNC_STOPPING':return m.actress_sync_error_network();
   default:return m.actress_sync_error_generic();
  }
 }
 $effect(() => {
  if (!dialog) return;
  if (c.isOpen && !dialog.open) {
   opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
   dialog.showModal(); dialog.querySelector<HTMLElement>('[data-sync-title]')?.focus();
  } else if (!c.isOpen && dialog.open) { dialog.close(); opener?.focus(); }
 });
 $effect(() => { const step = c.step; if (!c.isOpen) return; void tick().then(() => { if (dialog?.open && c.step === step) dialog.querySelector<HTMLElement>('[data-sync-step]')?.focus(); }); });
 $effect(() => { if (!c.isOpen || !c.recoveryReviewId) forget = false; });
 $effect(() => {
  const expiry = c.review?.expires_at;
  if (!c.isOpen || !expiry) return;
  const current = Date.now(); now = current; const delay = Date.parse(expiry) - current;
  if (Number.isFinite(delay) && delay > 0) { const timer = setTimeout(() => { now = Date.now(); }, Math.min(delay + 1, 2147483647)); return () => clearTimeout(timer); }
 });
 onDestroy(() => { c.close(); if (dialog?.open) dialog.close(); opener?.focus(); });
</script>
<dialog onkeydown={containTab} bind:this={dialog} class="sync-dialog" aria-labelledby="sync-title" aria-describedby="sync-scope" oncancel={(e) => { e.preventDefault(); c.close(); }}>
 {#if c.isOpen}
 <div class="shell">
  <header><h2 id="sync-title" data-sync-title tabindex="-1">{m.actress_sync_title()}</h2><Button variant="outline" onclick={c.close}>{m.actress_sync_close()}</Button></header>
  <div class="body stack">
   <p id="sync-scope">{m.actress_sync_scope()}</p>
   <nav class="actions" aria-label={m.actress_sync_title()}>
    {#if !['choose','review'].includes(c.step) && c.actors.length && !c.recoveryReviewId}<Button variant="outline" onclick={c.backToChoices} disabled={!!c.busy}>{m.actress_sync_back()}</Button>{/if}
    {#if c.step !== 'history'}<Button variant="outline" onclick={c.showHistory} disabled={!!c.busy}>{m.actress_sync_history()}</Button>{/if}
   </nav>
   <div class="actions">
    <Button variant="outline" onclick={c.loadInfo} disabled={!!c.busy || c.infoLoading}>{m.actress_sync_refresh_status()}</Button>
    {#if c.info?.enabled}<Button variant="outline" onclick={c.reloadEvidence} disabled={!!c.busy || c.infoLoading}>{c.errorCode === 'SYNC_RELOAD_FAILED' || c.errorCode === 'SYNC_RELOAD_BUSY' ? m.actress_sync_reload_retry() : m.actress_sync_reload()}</Button>{/if}
   </div>
   {#if c.info?.enabled}<p>{m.actress_sync_reload_help()}</p>{/if}
   {#if c.reloadResult}<p role="status" class="notice">{c.reloadResult === 'changed' ? m.actress_sync_reload_changed() : m.actress_sync_reload_unchanged()}</p>{/if}
   {#if c.info?.reload_problem_code && !c.errorCode}<p role="status" class="notice">{m.actress_sync_reload_failed()}</p>{/if}
   {#if c.infoLoading}<p role="status">{m.actress_sync_loading()}</p>
   {:else if c.info && !available}<aside class="notice stack"><p>{c.info.enabled ? m.actress_sync_unavailable() : m.actress_sync_disabled()}</p><p>{m.actress_sync_provisioning()}</p><a href="https://github.com/javinizer/javinizer-go/blob/main/docs/19-offline-actress-name-sync.md" target="_blank" rel="noopener noreferrer">{m.actress_sync_provisioning_docs()}</a></aside>{/if}
   {#if available && c.info && ['choose','review'].includes(c.step)}<aside class="notice stack"><p>{m.actress_sync_pinned()}</p>{#if c.info.recovered}<p>{m.actress_sync_recovered()}</p>{/if}<p class="break">{m.actress_sync_artifact()}: {c.info.artifact_digest}</p><p>{m.actress_sync_record_count({count:c.info.record_count})}</p></aside>{/if}
   {#if c.info?.worker_problem_code}<p role="status" class="notice">{m.actress_sync_worker_problem()}</p>{/if}
   {#if c.errorCode}<p class="error" role="alert">{errorLabel(c.errorCode)}</p>{/if}
   {#if c.busy}<p role="status">{m.actress_sync_working()}</p>{/if}
   {#if c.recoveryReviewId && c.busy !== 'confirm'}
    <aside class="notice stack"><p role="status">{m.actress_sync_uncertain()}</p><div class="actions"><Button onclick={c.retryConfirmation} disabled={!!c.busy}>{m.actress_sync_retry()}</Button><Button variant="outline" onclick={() => { forget = true; }} disabled={!!c.busy}>{m.actress_sync_forget()}</Button></div>
     {#if forget}<p role="alert">{m.actress_sync_forget_warning()}</p><div class="actions"><Button variant="outline" onclick={() => { forget = false; }} disabled={!!c.busy}>{m.actress_sync_keep_recovery()}</Button><Button variant="destructive" onclick={c.dismissRecovery} disabled={!!c.busy}>{m.actress_sync_confirm_forget()}</Button></div>{/if}
    </aside>
   {/if}
   {#if c.step === 'choose'}
    <section class="stack"><h3 data-sync-step tabindex="-1">{m.actress_sync_selection({count:c.actors.length})}</h3><p>{m.actress_sync_identity_help()}</p>
     {#if !c.actors.length}<p>{m.actress_sync_none_selected()}</p>{/if}
     <div class="choose-layout">
      <ul class="stack actors">{#each c.actors as actor (actor.id)}<li class="receipt stack">
       <button class="actor" aria-pressed={actor.id === c.activeActorId} aria-label={m.actress_sync_choose_for({name:actor.name})} onclick={() => c.selectActor(actor.id)} disabled={locked}><strong>{actor.name}</strong><span>{m.actress_sync_actor_id({id:actor.id})}</span><span lang="ja">{actor.japanese_name || m.actress_sync_empty_name()}</span><span>{c.choices[actor.id] ? m.actress_sync_selected() : m.actress_sync_required()}</span></button>
       <Button variant="ghost" onclick={() => c.removeActor(actor.id)} disabled={locked} aria-label={m.actress_sync_remove_actor({id:actor.id})}>{m.actress_sync_remove()}</Button>
      </li>{/each}</ul>
      {#if activeActor}<section class="stack source"><h4>{m.actress_sync_choose_for({name:activeActor.name})}</h4>
       {#if c.choices[activeActor.id]}<aside class="notice"><ActressNameSyncEvidence source={c.choices[activeActor.id]} /></aside>{/if}
       <form class="stack" onsubmit={(e) => {e.preventDefault(); void c.searchSources();}}><label for="sync-source-query">{m.actress_sync_search_label()}</label><input id="sync-source-query" type="search" value={c.query} oninput={(e) => c.setQuery(e.currentTarget.value)} disabled={locked || !available} /><Button type="submit" disabled={locked || !available || c.sourceLoading}>{m.actress_sync_search()}</Button></form>
       {#if c.sourceLoading}<p role="status">{m.actress_sync_loading()}</p>{/if}
       {#if c.searched && !c.sourceItems.length}<p>{m.actress_sync_no_sources()}</p>{/if}
       <ul class="stack">{#each c.sourceItems as source (source.source_key)}<li class="receipt stack"><ActressNameSyncEvidence {source}/><Button onclick={() => c.chooseSource(source)} disabled={locked}>{m.actress_sync_choose_source()}</Button></li>{/each}</ul>
       <div class="actions"><Button variant="outline" onclick={c.previousSources} disabled={!c.sourceHasPrevious || c.sourceLoading || locked}>{m.actress_sync_previous()}</Button><Button variant="outline" onclick={c.moreSources} disabled={c.sourceNextOffset === null || c.sourceLoading || locked}>{m.actress_sync_next()}</Button></div>
      </section>{/if}
     </div>
     <Button onclick={c.requestReview} disabled={!canReview}>{m.actress_sync_review()}</Button>
    </section>
   {:else if c.step === 'review' && c.review}
    <section class="stack"><h3 data-sync-step tabindex="-1">{m.actress_sync_review_title()}</h3><p>{m.actress_sync_review_help()}</p><p class="break">{m.actress_sync_artifact()}: {c.review.artifact_digest}</p><p>{m.actress_sync_max_age({seconds:c.review.max_age_seconds})}</p><p>{m.actress_sync_expires()}: {c.review.expires_at}</p>
     <ul class="stack">{#each c.review.proposals as proposal (proposal.actress_id)}<li class="receipt stack"><h4>{m.actress_sync_actor_id({id:proposal.actress_id})}</h4><p>{m.actress_sync_before()}: <span lang="ja">{proposal.before_name || m.actress_sync_empty_name()}</span></p><p>{m.actress_sync_after()}: <span lang="ja">{proposal.after_name || m.actress_sync_empty_name()}</span></p><ActressNameSyncEvidence source={proposal.source}/></li>{/each}</ul>
     {#if expired && !c.recoveryReviewId}<p role="alert">{m.actress_sync_expired()}</p>{/if}
     <label class="consent"><input type="checkbox" checked={c.consent} onchange={(e) => c.setConsent(e.currentTarget.checked)} disabled={locked || expired}/><span>{m.actress_sync_consent()}</span></label>
     <div class="actions"><Button variant="outline" onclick={c.backToChoices} disabled={locked}>{m.actress_sync_back()}</Button><Button onclick={c.confirm} disabled={!c.consent || locked || expired}>{m.actress_sync_confirm()}</Button></div>
    </section>
   {:else}<ActressNameSyncHistory controller={c}/>{/if}
  </div>
 </div>
 {/if}
</dialog>
<style>
 .sync-dialog{width:min(70rem,calc(100vw - 1.5rem));max-width:calc(100vw - 1.5rem);max-height:calc(100dvh - 1.5rem);padding:0;border:1px solid hsl(var(--border));border-radius:.75rem;color:hsl(var(--foreground));background:hsl(var(--background));overflow:hidden;margin:auto}.sync-dialog::backdrop{background:rgb(0 0 0 / .6)}.shell{display:flex;flex-direction:column;max-height:calc(100dvh - 1.5rem)}header{display:flex;justify-content:space-between;align-items:center;gap:1rem;padding:1rem;border-bottom:1px solid hsl(var(--border))}h2{font-size:1.2rem;font-weight:700}h3,h4{font-weight:600}.body{padding:1rem;overflow:auto;overscroll-behavior:contain}.stack{display:grid;gap:.75rem;min-width:0}.actions{display:flex;flex-wrap:wrap;gap:.5rem}.notice,.receipt{padding:.75rem;border:1px solid hsl(var(--border));border-radius:.5rem;min-width:0;overflow-wrap:anywhere}.notice{background:hsl(var(--muted))}.error{color:hsl(var(--destructive));overflow-wrap:anywhere}.break{overflow-wrap:anywhere}.choose-layout{display:grid;grid-template-columns:minmax(12rem,1fr) minmax(0,2fr);gap:1rem;align-items:start}ul{list-style:none;padding:0;margin:0}.actor{display:grid;text-align:left;gap:.25rem;width:100%;padding:.4rem}.actor[aria-pressed=true]{outline:2px solid hsl(var(--ring));border-radius:.25rem}.actor:disabled{opacity:.6}input[type=search]{padding:.5rem;border:1px solid hsl(var(--border));border-radius:.3rem;background:hsl(var(--background));width:100%;min-width:0}.consent{display:flex;align-items:flex-start;gap:.6rem}a{text-decoration:underline}:global(.sync-dialog button){white-space:normal;height:auto;min-height:2.25rem} @media(max-width:640px){.choose-layout{grid-template-columns:1fr}.actors{max-height:14rem;overflow:auto}header{padding:.75rem}.body{padding:.75rem}}
</style>
