<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { RefreshCw, ExternalLink } from 'lucide-svelte';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import type { VersionStatusResponse } from '$lib/api/types';

	// GitHub release URL for the Go rewrite. The latest tag is appended when
	// available so the "view release" link deep-links to the actual release.
	const REPO_RELEASES_URL = 'https://github.com/javinizer/javinizer-go/releases';
	const REPO_RELEASE_TAG_URL = 'https://github.com/javinizer/javinizer-go/releases/tag';

	interface Props {
		// The version status driving the CTA. When install_environment is
		// 'desktop' an in-app self-upgrade button renders; otherwise a "View
		// release" link to GitHub is shown instead.
		status: VersionStatusResponse | null;
		// Optional callback fired when the (desktop) self-upgrade transitions
		// in/out of its "Restarting…" state, so a parent can disable its own
		// interactions (e.g. keep an open popover from closing mid-upgrade).
		onUpgradingChange?: (upgrading: boolean) => void;
		// Optional callback fired when the CTA is clicked (e.g. to close a
		// popover after the user opens the releases link).
		onActivate?: () => void;
		// Visual scale: 'sm' (default) matches the nav popover's compact
		// "Check again" button; 'md' matches the settings card's text-sm scale.
		size?: 'sm' | 'md';
	}

	// Visual scale: 'sm' matches the nav popover's compact "Check again"
	// button; 'md' matches the settings card's `text-sm` control scale.
	let { status, onUpgradingChange, onActivate, size = 'sm' }: Props = $props();

	let upgrading = $state(false);

	function notify(upgrading: boolean) {
		if (onUpgradingChange) onUpgradingChange(upgrading);
	}

	async function handleUpgrade(event: MouseEvent) {
		event.stopPropagation();
		if (upgrading) return;
		upgrading = true;
		notify(true);
		try {
			const response = await apiClient.upgradeDesktop({ force: false });
			if (response.status === 'up-to-date') {
				upgrading = false;
				notify(false);
				toastStore.info(m.upgrade_already_up_to_date());
				return;
			}
			// On 200 'relaunching' the relaunch is already underway: hold the
			// "Restarting…" state while the old window closes and the new one opens.
		} catch (error) {
			upgrading = false;
			notify(false);
			const message = error instanceof Error ? error.message : m.upgrade_failed();
			toastStore.error(m.update_failed_prefix({ message }));
		}
	}

	const isDesktop = $derived(status?.install_environment === 'desktop');
	const releaseUrl = $derived(
		status?.latest ? `${REPO_RELEASE_TAG_URL}/${status.latest}` : REPO_RELEASES_URL
	);

	const sizeClass = $derived(
		size === 'md'
			? 'px-3 py-1.5 rounded-md text-sm'
			: 'px-2.5 py-1.5 rounded-md text-xs font-medium'
	);
</script>

{#if isDesktop}
	<button
		type="button"
		onclick={handleUpgrade}
		disabled={upgrading}
		aria-label={m.upgrade_aria_update_restart()}
		class="flex items-center gap-1.5 {sizeClass} transition-all duration-150 bg-primary text-primary-foreground hover:opacity-90 hover:translate-x-0.5 disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:translate-x-0"
	>
		<RefreshCw class="h-3.5 w-3.5 {upgrading ? 'animate-spin' : ''}" />
		{upgrading ? m.upgrade_restarting() : m.upgrade_update_restart()}
	</button>
{:else}
	<a
		href={releaseUrl}
		target="_blank"
		rel="noopener noreferrer"
		onclick={onActivate}
		class="flex items-center gap-1.5 {sizeClass} transition-all duration-150 bg-primary text-primary-foreground hover:opacity-90 hover:translate-x-0.5"
	>
		<ExternalLink class="h-3.5 w-3.5" />
		{m.upgrade_view_release()}
	</a>
{/if}
