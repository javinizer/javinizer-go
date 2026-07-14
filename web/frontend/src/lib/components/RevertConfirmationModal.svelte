<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { portalToBody } from '$lib/actions/portal';
	import { AlertTriangle, LoaderCircle } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		open: boolean;
		mode: 'batch' | 'operation';
		targetId: string;
		fileCount?: number;
		fileName?: string;
		onConfirm: () => Promise<void>;
		onCancel: () => void;
	}

	let { open = $bindable(false), mode, targetId, fileCount = 0, fileName = '', onConfirm, onCancel }: Props = $props();

	let reverting = $state(false);

	async function handleConfirm() {
		reverting = true;
		try {
			await onConfirm();
		} catch {
			// Error handling is done by the caller
		} finally {
			reverting = false;
		}
	}

	function handleCancel() {
		if (!reverting) {
			onCancel();
		}
	}
</script>

{#if open}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 bg-black/50 z-50"
		use:portalToBody
		in:fade|local={{ duration: 150 }}
		out:fade|local={{ duration: 120 }}
		onclick={handleCancel}
		onkeydown={(e) => { if (e.key === 'Escape') handleCancel(); }}
		role="presentation"
	>
		<div
			class="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-md p-4"
			in:scale|local={{ start: 0.97, duration: 190, easing: cubicOut }}
			out:scale|local={{ start: 1, opacity: 0.75, duration: 140, easing: cubicOut }}
			onclick={(e) => e.stopPropagation()}
			onkeydown={(e) => e.stopPropagation()}
			role="dialog"
			aria-modal="true"
			aria-labelledby="revert-modal-title"
			tabindex="-1"
		>
			<Card class="w-full">
				<!-- Header -->
				<div class="flex items-center gap-3 p-6 border-b">
					<div class="flex items-center justify-center w-10 h-10 rounded-full bg-amber-100 dark:bg-amber-900/30">
						<AlertTriangle class="h-5 w-5 text-amber-600 dark:text-amber-400" />
					</div>
					<h2 id="revert-modal-title" class="text-lg font-semibold">
						{#if mode === 'batch'}
							{m.revert_batch_title()}
						{:else}
							{m.revert_operation_title()}
						{/if}
					</h2>
				</div>

				<!-- Body -->
				<div class="p-6 space-y-4">
					<p class="text-sm text-muted-foreground">
						{#if mode === 'batch'}
							{m.revert_batch_body({ files: m.common_file_count({ count: fileCount }) })}
						{:else}
							{m.revert_operation_body({ fileName })}
						{/if}
					</p>

					<!-- Warning box -->
					<div class="rounded-lg bg-amber-50 dark:bg-amber-900/20 p-3 space-y-1">
						<p class="text-sm font-medium text-amber-800 dark:text-amber-300">{m.revert_consequences()}</p>
						<ul class="text-sm text-amber-700 dark:text-amber-400 list-disc list-inside space-y-0.5">
							<li>{m.revert_consequence_files()}</li>
							<li>{m.revert_consequence_nfos()}</li>
							<li>{m.revert_consequence_artwork()}</li>
						</ul>
					</div>
				</div>

				<!-- Footer -->
				<div class="flex items-center justify-end gap-3 p-6 border-t">
					<Button
						variant="outline"
						onclick={handleCancel}
						disabled={reverting}
					>
						{m.common_cancel()}
					</Button>
					<Button
						variant="destructive"
						onclick={handleConfirm}
						disabled={reverting}
						aria-label={mode === 'batch' ? m.revert_aria_batch({ count: fileCount }) : m.revert_aria_operation()}
					>
						{#if reverting}
							<LoaderCircle class="h-4 w-4 animate-spin" />
							{m.revert_reverting()}
						{:else if mode === 'batch'}
							{m.revert_button_batch({ count: fileCount })}
						{:else}
							{m.revert_button_operation()}
						{/if}
					</Button>
				</div>
			</Card>
		</div>
	</div>
{/if}
