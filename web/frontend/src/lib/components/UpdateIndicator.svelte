<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { onDestroy } from 'svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { ArrowUpCircle, RefreshCw, ChevronDown, Container, Monitor, Terminal, Copy, Check } from 'lucide-svelte';
	import { createVersionStatusQuery } from '$lib/query/queries';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import type { VersionStatusResponse } from '$lib/api/types';
	import UpgradeAction from '$lib/components/UpgradeAction.svelte';

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
						? `Prerelease ${data.latest} is available (current: ${data.current})`
						: `Update available: ${data.latest} (current: ${data.current})`
				);
			} else if (data.source === 'disabled') {
				toastStore.info('Update checks are disabled in configuration');
			} else if (data.latest) {
				toastStore.success(`You're running the latest version (${data.current})`);
			} else if (data.error) {
				toastStore.error(`Update check failed: ${data.error}`);
			}
		},
		onError: (error: Error) => {
			toastStore.error(`Update check failed: ${error.message}`);
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

	// Copy-to-clipboard for the upgrade instructions: docker/CLI users get a
	// multi-line command they can paste into a terminal. Shows a check-mark
	// confirmation for ~1.5s then reverts to the copy icon.
	let copiedInstructions = $state(false);
	let copyResetTimer: ReturnType<typeof setTimeout> | null = null;
	onDestroy(() => {
		if (copyResetTimer) clearTimeout(copyResetTimer);
	});
	async function handleCopyInstructions(event: MouseEvent) {
		event.stopPropagation();
		const text = status?.upgrade_instructions;
		if (!text) return;
		try {
			await navigator.clipboard.writeText(text);
			copiedInstructions = true;
			if (copyResetTimer) clearTimeout(copyResetTimer);
			copyResetTimer = setTimeout(() => (copiedInstructions = false), 1500);
		} catch {
			// clipboard unavailable (non-secure context) — surface it so the user
			// knows to select the command text manually instead.
			toastStore.error('Could not copy to clipboard — select the command text manually');
		}
	}

	// Environment label + icon for the "running in" badge. The backend classifies
	// docker/desktop/cli so the notification can tell a Docker user to `docker pull`
	// (an in-app self-upgrade is impossible for a read-only image) instead of
	// pointing them at a binary release asset they can't use.
	const envBadge = $derived.by(() => {
		switch (status?.install_environment) {
			case 'docker':
				return { label: 'Running in Docker', icon: Container, tone: 'bg-sky-500/15 text-sky-700 dark:text-sky-300' };
			case 'desktop':
				return { label: 'Desktop app', icon: Monitor, tone: 'bg-violet-500/15 text-violet-700 dark:text-violet-300' };
			default:
				return { label: 'CLI install', icon: Terminal, tone: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300' };
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
			aria-label="Update available"
			title={status?.prerelease ? `Prerelease ${status?.latest} available` : `Update ${status?.latest} available`}
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
				aria-label="Update details"
			>
				<div class="flex items-start gap-2">
					<ArrowUpCircle class="h-5 w-5 shrink-0 mt-0.5 text-primary" />
					<div class="min-w-0 flex-1">
						<p class="text-sm font-medium">
							{#if status?.prerelease}
								Prerelease available
							{:else}
								Update available
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
										prerelease
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

				{#if status?.upgrade_instructions && status?.install_environment !== 'desktop' && status?.install_environment !== 'docker'}
					<!-- Backend-provided, environment-specific guidance: CLI users see
					`javinizer upgrade`. Rendered verbatim (pre-wrap) so the indented
					commands stay readable. Desktop is excluded here: the "Update &
					restart" button below IS the self-upgrade, so a text block restating
					"click the button" (plus a long GitHub-download fallback) is
					redundant and noisy. Docker is excluded too: a user who ran
					`docker run` already knows to `docker pull` — the "View release"
					button covers the changelog. The API still returns guidance for
					the CLI `javinizer upgrade` handoff path
					(internal/update/upgrade.go), which has no button to defer to. -->
					<div
						class="mt-2 rounded-md bg-muted/60 border border-border overflow-hidden max-w-full"
					>
						<div class="flex items-center justify-between px-2 py-1 border-b border-border bg-muted">
							<span class="font-mono text-[10px] text-muted-foreground select-none">sh</span>
							<button
								type="button"
								onclick={handleCopyInstructions}
								title="Copy command"
								class="inline-flex items-center gap-1 px-1 py-0.5 rounded text-[10px] text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
							>
								{#if copiedInstructions}
									<Check class="h-3 w-3 text-emerald-500" />
									<span class="text-emerald-500">Copied</span>
								{:else}
									<Copy class="h-3 w-3" />
									<span>Copy</span>
								{/if}
							</button>
						</div>
						<pre
							class="block w-full max-w-full px-2.5 py-1.5 text-[11px] leading-relaxed font-mono whitespace-pre overflow-x-auto text-muted-foreground"
						>{status.upgrade_instructions}</pre
						>
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
						{checking ? 'Checking…' : 'Check again'}
					</button>
				</div>

				{#if status?.error}
					<p class="mt-2 text-[11px] text-destructive">{status.error}</p>
				{/if}
				{#if status?.checked_at}
					<p class="mt-2 text-[11px] text-muted-foreground">
						Last checked {new Date(status.checked_at).toLocaleString()}
					</p>
				{/if}
			</div>
		{/if}
	</div>
{/if}
