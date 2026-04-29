<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { portalToBody } from '$lib/actions/portal';
	import { AlertTriangle, Info } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { dialogs, dismissDialog } from '$lib/stores/dialog.svelte';
</script>

{#each dialogs as [id, dialog] (id)}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 bg-black/50 z-50"
		use:portalToBody
		in:fade|local={{ duration: 150 }}
		out:fade|local={{ duration: 120 }}
		onclick={() => dismissDialog(id, dialog.buttons[0]?.value ?? 'cancel')}
		onkeydown={(e) => { if (e.key === 'Escape') dismissDialog(id, dialog.buttons[0]?.value ?? 'cancel'); }}
		role="presentation"
	>
		<div
			class="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-md p-4"
			in:scale|local={{ start: 0.97, duration: 190, easing: cubicOut }}
			out:scale|local={{ start: 1, opacity: 0.75, duration: 140, easing: cubicOut }}
			onclick={(e) => e.stopPropagation()}
			onkeydown={(e) => e.stopPropagation()}
			role="alertdialog"
			aria-modal="true"
			aria-labelledby={`dialog-title-${id}`}
			aria-describedby={`dialog-desc-${id}`}
			tabindex="-1"
		>
			<Card class="w-full">
				<div class="flex items-center gap-3 p-6 border-b">
					<div class="flex items-center justify-center w-10 h-10 rounded-full {dialog.variant === 'danger' ? 'bg-amber-100 dark:bg-amber-900/30' : 'bg-blue-100 dark:bg-blue-900/30'}">
						{#if dialog.variant === 'danger'}
							<AlertTriangle class="h-5 w-5 text-amber-600 dark:text-amber-400" />
						{:else}
							<Info class="h-5 w-5 text-blue-600 dark:text-blue-400" />
						{/if}
					</div>
					<h2 id={`dialog-title-${id}`} class="text-lg font-semibold">
						{dialog.title}
					</h2>
				</div>

				<div class="p-6">
					<p id={`dialog-desc-${id}`} class="text-sm text-muted-foreground whitespace-pre-line">
						{dialog.message}
					</p>
				</div>

				<div class="flex items-center justify-end gap-3 p-6 border-t">
					{#each dialog.buttons as button}
						<Button
							variant={button.variant ?? 'default'}
							onclick={() => dismissDialog(id, button.value)}
						>
							{button.label}
						</Button>
					{/each}
				</div>
			</Card>
		</div>
	</div>
{/each}
