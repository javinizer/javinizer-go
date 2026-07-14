<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { formatDate } from '$lib/i18n/format';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { createApiTokensQuery } from '$lib/query/queries';
	import { createToken, revokeToken, regenerateToken } from '$lib/api/tokens';
	import type { CreateTokenResponse } from '$lib/types/token';
	import { toastStore } from '$lib/stores/toast';
	import { confirmDialog } from '$lib/stores/dialog.svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { Plus, Loader2, Trash2, RefreshCw } from 'lucide-svelte';

	interface Props {
		onTokenDisplay?: (response: CreateTokenResponse) => void;
	}

	let { onTokenDisplay }: Props = $props();

	const queryClient = useQueryClient();

	const tokensQuery = createApiTokensQuery();
	let tokens = $derived(tokensQuery.data?.tokens ?? []);
	let loading = $derived(tokensQuery.isPending);
	let error = $derived<string | null>(tokensQuery.error?.message ?? null);

	let newTokenName = $state('');

	const createTokenMutation = createMutation(() => ({
		mutationFn: (name?: string) => createToken(name),
		onSuccess: (data: CreateTokenResponse) => {
			newTokenName = '';
			toastStore.success(m.settings_api_token_created_toast(), 3000);
			onTokenDisplay?.(data);
			void queryClient.invalidateQueries({ queryKey: ['api-tokens'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.settings_api_token_create_failed(), 4000);
		}
	}));

	const revokeTokenMutation = createMutation(() => ({
		mutationFn: (id: string) => revokeToken(id),
		onSuccess: () => {
			toastStore.success(m.settings_api_token_revoked_toast(), 3000);
			void queryClient.invalidateQueries({ queryKey: ['api-tokens'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.settings_api_token_revoke_failed(), 4000);
		}
	}));

	const regenerateTokenMutation = createMutation(() => ({
		mutationFn: (id: string) => regenerateToken(id),
		onSuccess: (data: CreateTokenResponse) => {
			toastStore.success(m.settings_api_token_regenerated_toast(), 3000);
			onTokenDisplay?.(data);
			void queryClient.invalidateQueries({ queryKey: ['api-tokens'] });
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.settings_api_token_regenerate_failed(), 4000);
		}
	}));

	async function handleCreate() {
		createTokenMutation.mutate(newTokenName.trim() || undefined);
	}

	async function handleRevoke(id: string, name: string) {
		const confirmed = await confirmDialog(
			m.settings_api_token_revoke_confirm_title(),
			m.settings_api_token_revoke_confirm_desc({ name: name || id }),
			{ confirmLabel: m.settings_api_token_revoke_action(), variant: 'danger' }
		);
		if (confirmed) {
			revokeTokenMutation.mutate(id);
		}
	}

	async function handleRegenerate(id: string, name: string) {
		const confirmed = await confirmDialog(
			m.settings_api_token_regenerate_confirm_title(),
			m.settings_api_token_regenerate_confirm_desc({ name: name || id }),
			{ confirmLabel: m.settings_api_token_regenerate_action(), variant: 'danger' }
		);
		if (confirmed) {
			regenerateTokenMutation.mutate(id);
		}
	}

	function formatTokenDate(dateStr: string | null): string {
		if (!dateStr) return m.settings_api_token_never_used();
		try {
			return formatDate(dateStr, {
				year: 'numeric',
				month: 'short',
				day: 'numeric',
				hour: '2-digit',
				minute: '2-digit'
			});
		} catch {
			return dateStr;
		}
	}

	function handleCreateKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			handleCreate();
		}
	}
</script>

<SettingsSection title={m.settings_api_tokens_title()} description={m.settings_api_tokens_desc()} defaultExpanded={false}>
	{#if loading}
		<div class="flex items-center justify-center py-8 text-muted-foreground">
			<Loader2 class="h-5 w-5 animate-spin mr-2" />
			{m.common_loading()}
		</div>
	{:else if error}
		<div class="text-destructive text-sm py-4">
			{m.settings_api_tokens_load_failed({ error })}
		</div>
	{:else}
		<div class="space-y-4">
			{#if tokens.length === 0}
				<p class="text-sm text-muted-foreground py-4">
					{m.settings_api_tokens_empty()}
				</p>
			{:else}
				<div class="relative border border-border rounded-lg overflow-hidden">
					<div class="overflow-x-auto">
						<table class="w-full text-sm">
							<thead>
								<tr class="border-b border-border bg-muted/50">
									<th class="text-left py-2 px-3 font-medium text-muted-foreground">{m.settings_api_tokens_col_name()}</th>
									<th class="text-left py-2 px-3 font-medium text-muted-foreground">{m.settings_api_tokens_col_prefix()}</th>
									<th class="text-left py-2 px-3 font-medium text-muted-foreground">{m.settings_api_tokens_col_created()}</th>
									<th class="text-left py-2 px-3 font-medium text-muted-foreground">{m.settings_api_tokens_col_last_used()}</th>
									<th class="text-right py-2 px-3 font-medium text-muted-foreground">{m.common_actions()}</th>
								</tr>
							</thead>
							<tbody>
								{#each tokens as token (token.id)}
									<tr class="border-b border-border/50 hover:bg-accent/30 transition-colors">
										<td class="py-2 px-3">{#if token.name}{token.name}{:else}<span class="text-muted-foreground italic">{m.settings_api_token_unnamed()}</span>{/if}</td>
										<td class="py-2 px-3 font-mono text-xs">{token.token_prefix}</td>
										<td class="py-2 px-3 text-xs">{formatTokenDate(token.created_at)}</td>
										<td class="py-2 px-3 text-xs">{formatTokenDate(token.last_used_at)}</td>
										<td class="py-2 px-3 text-right">
											<div class="flex items-center justify-end gap-1">
												<button
													type="button"
													class="text-muted-foreground hover:text-foreground transition-colors p-1 rounded"
													title={m.settings_api_token_regenerate_tooltip()}
													onclick={() => handleRegenerate(token.id, token.name)}
													disabled={regenerateTokenMutation.isPending}
												>
													<RefreshCw class="h-4 w-4" />
												</button>
												<button
													type="button"
													class="text-muted-foreground hover:text-destructive transition-colors p-1 rounded"
													title={m.settings_api_token_revoke_tooltip()}
													onclick={() => handleRevoke(token.id, token.name)}
													disabled={revokeTokenMutation.isPending}
												>
													<Trash2 class="h-4 w-4" />
												</button>
											</div>
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
				<p class="text-xs text-muted-foreground">
					{m.settings_api_tokens_active_count({ count: tokens.length })}
				</p>
			{/if}

			<div class="border-t pt-4">
				<p class="text-xs text-muted-foreground mb-3">{m.settings_api_tokens_create_new()}</p>
				<div class="flex items-end gap-2">
					<div class="flex-1">
						<label for="token-name" class="block text-xs font-medium text-muted-foreground mb-1">{m.settings_api_token_name_optional()}</label>
						<input
							id="token-name"
							type="text"
							bind:value={newTokenName}
							placeholder={m.settings_api_token_name_placeholder()}
							onkeydown={handleCreateKeydown}
							class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring"
						/>
					</div>
					<Button
						size="sm"
						onclick={handleCreate}
						disabled={createTokenMutation.isPending}
					>
						{#if createTokenMutation.isPending}
							<Loader2 class="h-4 w-4 animate-spin mr-1" />
						{:else}
							<Plus class="h-4 w-4 mr-1" />
						{/if}
						{m.settings_api_token_create_button()}
					</Button>
				</div>
			</div>

			<p class="text-xs text-muted-foreground">
				{m.settings_api_tokens_prefix_note()}
			</p>
		</div>
	{/if}
</SettingsSection>
