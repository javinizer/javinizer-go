<script lang="ts">
	import { quintOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { FolderOpen, X } from 'lucide-svelte';
	import { portalToBody } from '$lib/actions/portal';
	import type { OrganizePreviewResponse } from '$lib/api/types';
	import Button from '$lib/components/ui/Button.svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import OrganizePreviewTree from './OrganizePreviewTree.svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		show: boolean;
		preview: OrganizePreviewResponse | null;
		destinationPath: string;
		previewNeedsDestination: boolean;
		onClose: () => void;
	}

	let {
		show = $bindable(false),
		preview,
		destinationPath,
		previewNeedsDestination,
		onClose
	}: Props = $props();

	function close() {
		show = false;
	}

	function handleKey(e: KeyboardEvent) {
		if (!show) return;
		if (e.key === 'Escape') close();
	}

	// Map raw API enums to localized labels — the preview must never print
	// tokens like `prefer-nfo` or label every override "Force overwrite".
	// Unknown/future values render raw (identifiable) rather than hidden.
	const nfoLabels: Record<string, () => string> = {
		write: m.browse_plan_nfo_write,
		skip: m.browse_plan_nfo_skip
	};
	const mediaLabels: Record<string, () => string> = {
		missing: m.browse_plan_media_missing,
		replace: m.browse_plan_media_replace,
		skip: m.browse_plan_media_skip
	};
	const scalarLabels: Record<string, () => string> = {
		'prefer-nfo': m.browse_prefer_nfo,
		'prefer-scraper': m.browse_prefer_scraped,
		'preserve-existing': m.browse_preserve_existing,
		'fill-missing-only': m.browse_fill_missing_only
	};
	const arrayLabels: Record<string, () => string> = { merge: m.browse_merge, replace: m.browse_replace };
	const overrideLabels: Record<string, () => string> = {
		none: m.review_merge_override_none,
		'force-overwrite': m.review_force_overwrite,
		'preserve-nfo': m.review_preserve_nfo
	};
	function labelFor(map: Record<string, () => string>, value: string): string {
		return map[value]?.() ?? value;
	}
</script>

<svelte:window onkeydown={handleKey} />

{#if show}
	<div
		class="fixed inset-0 bg-black/60 backdrop-blur-sm z-50 flex items-center justify-center p-4"
		use:portalToBody
		in:fade|local={{ duration: 140 }}
		out:fade|local={{ duration: 120 }}
		role="presentation"
		onclick={(e) => { if (e.target === e.currentTarget) close(); }}
	>
		<div
			class="w-full max-w-4xl"
			role="dialog"
			aria-modal="true"
			aria-labelledby="output-preview-modal-title"
			in:scale|local={{ start: 0.97, duration: 180, easing: quintOut }}
			out:scale|local={{ start: 1, opacity: 0.7, duration: 130, easing: quintOut }}
		>
			<Card class="w-full flex flex-col max-h-[90vh] overflow-hidden">
				<div class="px-6 py-4 border-b flex items-center justify-between gap-4">
					<div class="flex items-center gap-3 min-w-0">
						<div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
							<FolderOpen class="h-4 w-4" />
						</div>
						<div class="min-w-0">
							<h2 id="output-preview-modal-title" class="text-lg font-semibold tracking-tight truncate">
								{m.review_output_preview_title()}
							</h2>
							<p class="text-xs text-muted-foreground truncate">{destinationPath || '—'}</p>
						</div>
					</div>
					<Button variant="ghost" size="icon" onclick={close} aria-label={m.common_close()}>
						{#snippet children()}<X class="h-4 w-4" />{/snippet}
					</Button>
				</div>

				<div class="flex-1 overflow-y-auto p-6">
					{#if preview?.effective_apply}
						<div class="mb-4 rounded-lg border border-primary/20 bg-primary/5 p-4 text-sm">
							<h3 class="font-semibold">{m.browse_plan_summary_title()}</h3>
							<ul class="mt-2 space-y-1">
								<li>{m.browse_plan_nfo_output()}: {labelFor(nfoLabels, preview.effective_apply.plan.nfo_output)}</li>
								<li>{m.browse_plan_media_policy()}: {labelFor(mediaLabels, preview.effective_apply.plan.media_policy)}</li>
								{#if preview.effective_apply.plan.merge}<li>{m.browse_plan_existing_merge()}: {labelFor(scalarLabels, preview.effective_apply.plan.merge.scalar_strategy)}, {labelFor(arrayLabels, preview.effective_apply.plan.merge.array_strategy)}</li>{/if}
								<li>{m.review_merge_override_label()}: {labelFor(overrideLabels, preview.effective_apply.merge_override)}</li>
							</ul>
							{#if preview.effective_apply.plan.media_policy === 'replace'}<p class="mt-3 font-medium text-destructive" role="alert">{m.review_media_replace_warning()}</p>{/if}
						</div>
					{/if}
					{#if previewNeedsDestination && !destinationPath.trim()}
						<div class="text-center py-12 text-muted-foreground">
							<p>{m.review_output_preview_empty()}</p>
						</div>
					{:else}
						<OrganizePreviewTree {preview} {destinationPath} />
					{/if}
				</div>

				<div class="px-6 py-3 border-t flex items-center justify-end gap-3">
					<Button variant="outline" onclick={close}>
						{#snippet children()}{m.common_close()}{/snippet}
					</Button>
				</div>
			</Card>
		</div>
	</div>
{/if}