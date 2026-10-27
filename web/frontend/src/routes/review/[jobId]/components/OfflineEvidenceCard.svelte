<script lang="ts">
	import type { OfflineEvidenceReport } from '$lib/api/types';
	import Card from '$lib/components/ui/Card.svelte';
	import { offlineEvidenceOutcome } from '$lib/offline-evidence';
	import * as m from '$lib/paraglide/messages';

	let { report }: { report?: OfflineEvidenceReport } = $props();
</script>

{#if report}
	<section aria-label={m.offline_evidence_title()}>
		<Card class="space-y-4 p-6">
			<div>
				<h2 class="text-lg font-semibold">{m.offline_evidence_title()}</h2>
				<p class="mt-1 text-sm text-muted-foreground">{m.offline_evidence_read_only()}</p>
			</div>
			<p class="rounded-md border bg-muted/40 p-3 text-sm">
				{offlineEvidenceOutcome(report.outcome)}
			</p>
			{#if report.recovered}
				<p class="text-sm text-muted-foreground">{m.offline_evidence_recovered()}</p>
			{/if}
			{#if report.observations?.length}
				<ul class="space-y-3">
					{#each report.observations as observation}
						<li class="space-y-2 rounded-md border p-3 text-sm">
							<p class="font-medium">
								{m.offline_evidence_reference()}:
								<code class="break-all">{observation.source}{observation.source_id ? `:${observation.source_id}` : ''}</code>
							</p>
							<p class="text-muted-foreground">{offlineEvidenceOutcome(observation.outcome)}</p>
							{#if observation.outcome === 'evidence_available'}
								{#if observation.japanese_name}
									<p>
										<span class="text-muted-foreground">{m.offline_evidence_japanese_name()}:</span>
										<span class="break-words">{observation.japanese_name}</span>
									</p>
								{/if}
								{#if observation.aliases?.length}
									<div>
										<p class="text-muted-foreground">{m.offline_evidence_aliases()}:</p>
										<ul class="list-inside list-disc break-words">
											{#each observation.aliases as alias}<li>{alias}</li>{/each}
										</ul>
									</div>
								{/if}
							{/if}
							{#if observation.observed_at}
								<p class="text-xs text-muted-foreground">
									{m.offline_evidence_observed()}:
									<time datetime={observation.observed_at}>{observation.observed_at}</time>
								</p>
							{/if}
							{#if observation.source_url || observation.dataset_sha256 || observation.parser_revision}
								<details class="text-xs text-muted-foreground">
									<summary class="cursor-pointer">{m.offline_evidence_provenance()}</summary>
									<dl class="mt-2 space-y-2 break-all">
										{#if observation.source_url}
											<div><dt>{m.offline_evidence_source_url()}</dt><dd>{observation.source_url}</dd></div>
										{/if}
										{#if observation.dataset_sha256}
											<div><dt>{m.offline_evidence_dataset()}</dt><dd><code>{observation.dataset_sha256}</code></dd></div>
										{/if}
										{#if observation.parser_revision}
											<div><dt>{m.offline_evidence_parser()}</dt><dd>{observation.parser_revision}</dd></div>
										{/if}
									</dl>
								</details>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
			<details class="text-xs text-muted-foreground">
				<summary class="cursor-pointer">{m.offline_evidence_snapshot()}</summary>
				<dl class="mt-2 space-y-2 break-all">
					<div>
						<dt>{m.offline_evidence_admitted()}</dt>
						<dd><time datetime={report.admitted_at}>{report.admitted_at}</time></dd>
					</div>
					{#if report.snapshot_digest}
						<div><dt>{m.offline_evidence_digest()}</dt><dd><code>{report.snapshot_digest}</code></dd></div>
					{/if}
				</dl>
			</details>
		</Card>
	</section>
{/if}
