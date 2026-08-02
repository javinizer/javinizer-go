<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { formatDateTime } from '$lib/i18n/format';
	import { cubicOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { ArrowUpCircle, RefreshCw, Container, Monitor, Terminal } from 'lucide-svelte';
	import { createVersionStatusQuery } from '$lib/query/queries';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import type { VersionStatusResponse } from '$lib/api/types';
	import UpgradeAction from '$lib/components/UpgradeAction.svelte';
	import UpgradeCommandList from '$lib/components/UpgradeCommandList.svelte';

	const queryClient = useQueryClient();
	const versionQuery = $derived(createVersionStatusQuery());
	const status = $derived(versionQuery.data ?? null);

	// Only surface the indicator when an update is genuinely available. Hidden
	// when: query is pending/errored, checks are disabled (source === 'disabled'),
	// no state yet (source === 'none'), or update_available is false. This keeps
	// the nav clean for up-to-date / disabled / offline users.
	const showIndicator = $derived(
		!!status && status.update_available && status.source !== 'disabled' && status.source !== 'none'
	);

	let popoverOpen = $state(false);

	// Desktop self-upgrade state. Driven by UpgradeAction's onUpgradingChange
	// callback so this popover stays locked (no toggling/closing) while the
	// relaunch is underway: the old window closes and a new one opens.
	let upgrading = $state(false);

	// Force a fresh check (POST /api/v1/version/check hits GitHub with the
	// server-side rate limit/cache). Invalidates the status query on success so
	// the indicator updates immediately.
	const checkMutation = createMutation(() => ({
		mutationFn: () => apiClient.checkVersion(),
		onSuccess: (data: VersionStatusResponse) => {
			queryClient.invalidateQueries({ queryKey: ['version-status'] });
			if (data.update_available) {
				toastStore.info(
					data.prerelease
						? m.update_toast_prerelease({ latest: data.latest, current: data.current })
						: m.update_toast_available({ latest: data.latest, current: data.current })
				);
			} else if (data.source === 'disabled') {
				toastStore.info(m.update_toast_checks_disabled());
			} else if (data.latest) {
				toastStore.success(m.update_toast_latest({ current: data.current }));
			} else if (data.error) {
				toastStore.error(m.update_toast_check_failed({ error: data.error }));
			}
		},
		onError: (error: Error) => {
			toastStore.error(m.update_toast_check_failed({ error: error.message }));
		},
	}));

	function togglePopover() {
		if (upgrading) return;
		popoverOpen = !popoverOpen;
	}

	function closePopover() {
		popoverOpen = false;
	}

	function handleClickOutside(event: MouseEvent) {
		if (upgrading) return;
		const target = event.target as HTMLElement;
		if (!target.closest('[data-update-indicator]')) {
			closePopover();
		}
	}

	function handleCheckNow(event: MouseEvent) {
		event.stopPropagation();
		checkMutation.mutate();
	}

	const checking = $derived(checkMutation.isPending);

	// Environment label + icon for the "running in" badge. The backend classifies
	// docker/desktop/cli so the notification can tell a Docker user to `docker pull`
	// (an in-app self-upgrade is impossible for a read-only image) instead of
	// pointing them at a binary release asset they can't use.
	const envBadge = $derived.by(() => {
		switch (status?.install_environment) {
			case 'docker':
				return { label: m.update_env_docker(), icon: Container, tone: 'bg-sky-500/15 text-sky-700 dark:text-sky-300' };
			case 'desktop':
				return { label: m.update_env_desktop(), icon: Monitor, tone: 'bg-violet-500/15 text-violet-700 dark:text-violet-300' };
			default:
				return { label: m.update_env_cli(), icon: Terminal, tone: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300' };
		}
	});
</script>

<svelte:window onclick={handleClickOutside} onkeydown={(e) => { if (e.key === 'Escape' && popoverOpen && !upgrading) popoverOpen = false; }} />

{#if showIndicator}
	<div class="relative" data-update-indicator>
		<button
			type="button"
			onclick={togglePopover}
			aria-expanded={popoverOpen}
			aria-haspopup="true"
			aria-label={m.update_aria_available()}
			title={status?.prerelease ? m.update_title_prerelease_available({ version: status?.latest ?? '' }) : m.update_title_update_available({ version: status?.latest ?? '' })}
			class="relative flex items-center justify-center h-9 w-9 rounded-md transition-all duration-200 hover:bg-accent hover:-translate-y-px text-primary"
		>
			<ArrowUpCircle class="h-5 w-5" />
			<!-- Pulsing dot: draws the eye without a full banner. Prerelease uses
			amber, stable uses primary, matching the popover tag below. -->
			<span
				class="absolute top-1 right-1 h-2 w-2 rounded-full {status?.prerelease
					? 'bg-amber-500'
					: 'bg-primary'} animate-pulse"
			></span>
		</button>

		{#if popoverOpen}
			<div
				class="absolute right-0 top-full mt-1 w-80 rounded-lg border bg-card p-3 shadow-lg z-50"
				in:fly={{ y: -4, duration: 120, easing: cubicOut }}
				role="dialog"
				aria-label={m.update_aria_details()}
			>
				<div class="flex items-start gap-2">
					<ArrowUpCircle class="h-5 w-5 shrink-0 mt-0.5 text-primary" />
					<div class="min-w-0 flex-1">
						<p class="text-sm font-medium">
							{#if status?.prerelease}
								{m.update_prerelease_available()}
							{:else}
								{m.update_available()}
							{/if}
						</p>
						<p class="mt-1 text-xs text-muted-foreground">
							<span class="font-mono text-foreground/80">{status?.current}</span>
							<span class="mx-1.5 text-muted-foreground/60">→</span>
							<span class="font-mono font-medium text-primary">{status?.latest}</span>
						</p>
						{#if status?.prerelease || status?.install_environment}
							<div class="mt-2 flex flex-wrap items-center gap-1.5">
								{#if status?.prerelease}
									<span
										class="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-500/15 text-amber-700 dark:text-amber-300"
									>
										{m.update_prerelease_tag()}
									</span>
								{/if}
								{#if status?.install_environment}
									{@const Badge = envBadge.icon}
									<span
										class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium {envBadge.tone}"
										title={envBadge.label}
									>
										<Badge class="h-3 w-3" />
										{envBadge.label}
									</span>
								{/if}
							</div>
						{/if}
					</div>
				</div>

				{#if status?.upgrade_commands?.length && status?.install_environment !== 'desktop'}
					<!-- Backend-provided, environment-aware command rows: each row is
					a complete, paste-ready command with its OWN copy button — the
					structured replacement for the old `sh`-labeled prose blob that
					couldn't actually be pasted into a terminal. Desktop is excluded:
					the "Update & restart" button below IS the self-upgrade (the API
					also returns no commands for it). Docker now gets pull/compose
					rows: the earlier "docker users already know" hiding made sense
					for noisy prose, but discrete copyable commands are terse enough
					to earn their space. -->
					<p class="mt-2 text-[11px] text-muted-foreground">
						{status.install_environment === 'docker' ? m.update_cmd_lead_docker() : m.update_cmd_lead_cli()}
					</p>
					<div class="mt-1">
						<UpgradeCommandList commands={status.upgrade_commands} />
					</div>
				{/if}

				<div class="mt-3 flex items-center gap-2">
					<UpgradeAction
						{status}
						onUpgradingChange={(u) => (upgrading = u)}
						onActivate={closePopover}
					/>
					<button
						type="button"
						onclick={handleCheckNow}
						disabled={checking || upgrading}
						class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-all duration-150 hover:bg-accent hover:translate-x-0.5 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:translate-x-0"
					>
						<RefreshCw class="h-3.5 w-3.5 {checking ? 'animate-spin' : ''}" />
						{checking ? m.update_checking() : m.update_check_again()}
					</button>
				</div>

				{#if status?.error}
					<p class="mt-2 text-[11px] text-destructive">{status.error}</p>
				{/if}
				{#if status?.checked_at}
					<p class="mt-2 text-[11px] text-muted-foreground">
						{m.update_last_checked({ datetime: formatDateTime(status.checked_at) })}
					</p>
				{/if}
			</div>
		{/if}
	</div>
{/if}
