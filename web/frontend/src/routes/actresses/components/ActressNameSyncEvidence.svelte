<script lang="ts">
 import type { ActressNameSyncSource } from '$lib/api/types/actress-name-sync';
 import * as m from '$lib/paraglide/messages';
 let { source }: { source: ActressNameSyncSource } = $props();
 // Provenance is display-only; never render an executable source-provided scheme.
 let safeURL = $derived.by(() => {
  try { const u = new URL(source.source_url); return ['https:', 'http:'].includes(u.protocol) && !u.username && !u.password ? u.href : null; } catch { return null; }
 });
</script>
<dl class="evidence">
 <dt>{m.actress_sync_source_id()}</dt><dd>{source.source_id}</dd>
 <dt>{m.actress_sync_source_key()}</dt><dd>{source.source_key}</dd>
 <dt>{m.actress_sync_japanese_name()}</dt><dd lang="ja">{source.japanese_name || m.actress_sync_empty_name()}</dd>
 <dt>{m.actress_sync_aliases()}</dt><dd>{source.aliases?.join(' · ') || '—'}</dd>
 <dt>{m.actress_sync_observed()}</dt><dd>{source.observed_at}</dd>
 <dt>{m.actress_sync_provenance()}</dt><dd>{#if safeURL}<a href={safeURL} target="_blank" rel="noopener noreferrer">{source.source_url}</a>{:else}{source.source_url || '—'}{/if}</dd>
</dl>
<style>
 .evidence { display:grid;grid-template-columns:minmax(6rem,1fr) minmax(0,3fr);gap:.3rem .75rem;font-size:.875rem; }
 dt { color:hsl(var(--muted-foreground)); } dd { margin:0;overflow-wrap:anywhere; } a { text-decoration:underline; }
 @media(max-width:480px){.evidence{grid-template-columns:1fr;gap:.15rem;}dd{margin-bottom:.4rem;}}
</style>
