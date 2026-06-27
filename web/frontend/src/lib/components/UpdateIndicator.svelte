<script lang="ts">
	import { cubicOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { ArrowUpCircle, RefreshCw, ExternalLink, ChevronDown } from 'lucide-svelte';
	import { createVersionStatusQuery } from '$lib/query/queries';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import type { VersionStatusResponse } from '$lib/api/types';

	// GitHub release URL for the Go rewrite. The latest tag is appended when
	// available so the "view release" link deep-links to the actual release.
	const REPO_RELEASES_URL = 'https://github.com/javinizer/javinizer-go/releases';
	const REPO_RELEASE_TAG_URL = 'https://github.com/javinizer/javinizer-go/releases/tag';

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
		popoverOpen = !popoverOpen;
	}

	function closePopover() {
		popoverOpen = false;
	}

	function handleClickOutside(event: MouseEvent) {
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
	const releaseUrl = $derived(
		status?.latest ? `${REPO_RELEASE_TAG_URL}/${status.latest}` : REPO_RELEASES_URL
	);
</script>

<svelte:window onclick={handleClickOutside} onkeydown={(e) => { if (e.key === 'Escape' && popoverOpen) popoverOpen = false; }} />

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
				class="absolute right-0 top-full mt-1 w-72 rounded-lg border bg-card p-3 shadow-lg z-50"
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
						<p class="mt-1 text-xs text-muted-foreground break-all">
							<span class="font-medium text-foreground">{status?.latest}</span>
							<span class="mx-1">·</span>
							current <span class="font-medium text-foreground">{status?.current}</span>
						</p>
						{#if status?.prerelease}
							<span
								class="inline-block mt-2 px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-500/15 text-amber-700 dark:text-amber-300"
							>
								prerelease
							</span>
						{/if}
					</div>
				</div>

				<div class="mt-3 flex items-center gap-2">
					<a
						href={releaseUrl}
						target="_blank"
						rel="noopener noreferrer"
						onclick={closePopover}
						class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-all duration-150 bg-primary text-primary-foreground hover:opacity-90 hover:translate-x-0.5"
					>
						<ExternalLink class="h-3.5 w-3.5" />
						View release
					</a>
					<button
						type="button"
						onclick={handleCheckNow}
						disabled={checking}
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
