<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fade, scale } from 'svelte/transition';
	import { portalToBody } from '$lib/actions/portal';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { AlertTriangle, Copy, Check, X } from 'lucide-svelte';
	import type { CreateTokenResponse } from '$lib/types/token';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		tokenResponse: CreateTokenResponse | null;
		onClose: () => void;
	}

	let { tokenResponse, onClose }: Props = $props();

	let copied = $state(false);
	let copyTimeout: ReturnType<typeof setTimeout> | null = null;

	async function copyToClipboard() {
		if (!tokenResponse?.token) return;
		try {
			await navigator.clipboard.writeText(tokenResponse.token);
			copied = true;
			if (copyTimeout) clearTimeout(copyTimeout);
			copyTimeout = setTimeout(() => { copied = false; }, 2000);
		} catch {
			const input = document.getElementById('token-value-input') as HTMLInputElement | null;
			if (input) {
				input.select();
				document.execCommand('copy');
				copied = true;
				if (copyTimeout) clearTimeout(copyTimeout);
				copyTimeout = setTimeout(() => { copied = false; }, 2000);
			}
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			onClose();
		}
	}
</script>

{#if tokenResponse}
	<!-- svelte-ignore a11y_click_events_have_key_events -->
	<div
		class="fixed inset-0 bg-black/50 z-50"
		use:portalToBody
		in:fade|local={{ duration: 150 }}
		out:fade|local={{ duration: 120 }}
		onclick={onClose}
		onkeydown={handleKeydown}
		role="presentation"
	>
		<div
			class="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-full max-w-lg p-4"
			in:scale|local={{ start: 0.97, duration: 190, easing: cubicOut }}
			out:scale|local={{ start: 1, opacity: 0.75, duration: 140, easing: cubicOut }}
			onclick={(e) => e.stopPropagation()}
			onkeydown={(e) => e.stopPropagation()}
			role="dialog"
			aria-modal="true"
			aria-label={m.settings_token_modal_aria()}
			tabindex="-1"
		>
			<Card class="w-full">
				<div class="flex items-center justify-between p-6 border-b">
					<div class="flex items-center gap-3">
						<div class="flex items-center justify-center w-10 h-10 rounded-full bg-amber-100 dark:bg-amber-900/30">
							<AlertTriangle class="h-5 w-5 text-amber-600 dark:text-amber-400" />
						</div>
						<h2 class="text-lg font-semibold">
							{m.settings_token_created_title()}
						</h2>
					</div>
					<button
						type="button"
						class="text-muted-foreground hover:text-foreground transition-colors p-1 rounded"
						onclick={onClose}
						aria-label={m.common_close()}
					>
						<X class="h-5 w-5" />
					</button>
				</div>

				<div class="p-6 space-y-4">
					<div class="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg p-3">
						<p class="text-sm text-amber-800 dark:text-amber-200 font-medium">
							{m.settings_token_warning()}
						</p>
					</div>

					{#if tokenResponse.name}
						<div>
							<label for="token-name-display" class="block text-xs font-medium text-muted-foreground mb-1">{m.settings_token_name_label()}</label>
							<p id="token-name-display" class="text-sm">{tokenResponse.name}</p>
						</div>
					{/if}

					<div>
						<label for="token-value-input" class="block text-xs font-medium text-muted-foreground mb-1">{m.settings_token_value_label()}</label>
						<div class="flex items-center gap-2">
							<input
								id="token-value-input"
								type="text"
								readonly
								value={tokenResponse.token}
								class="flex-1 rounded-md border border-input bg-muted px-3 py-2 text-sm font-mono focus:outline-none"
							/>
							<Button
								variant={copied ? 'default' : 'outline'}
								size="sm"
								onclick={copyToClipboard}
							>
								{#if copied}
									<Check class="h-4 w-4 mr-1" />
									{m.settings_token_copied()}
								{:else}
									<Copy class="h-4 w-4 mr-1" />
									{m.settings_token_copy()}
								{/if}
							</Button>
						</div>
					</div>
				</div>

				<div class="flex items-center justify-end p-6 border-t">
					<Button variant="outline" onclick={onClose}>
						{m.common_close()}
					</Button>
				</div>
			</Card>
		</div>
	</div>
{/if}
