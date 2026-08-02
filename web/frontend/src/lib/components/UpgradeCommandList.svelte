<script lang="ts">
	import { onDestroy } from 'svelte';
	import { Copy, Check } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages';
	import { toastStore } from '$lib/stores/toast';
	import type { UpgradeCommand } from '$lib/api/types';

	let { commands }: { commands: UpgradeCommand[] } = $props();

	// The backend sends a stable semantic key per row (see
	// internal/system/environment.go UpgradeCommands) so labels can be
	// localized here instead of shipping English strings from the API.
	// Unknown keys fall back to the raw key — forward-compatible if the
	// backend adds a variant before this map is updated.
	const labelByKey: Record<string, () => string> = {
		cli_binary: m.update_cmd_label_cli_binary,
		homebrew: m.update_cmd_label_homebrew,
		scoop: m.update_cmd_label_scoop,
		docker_pull: m.update_cmd_label_docker_pull,
		docker_compose: m.update_cmd_label_docker_compose,
	};
	function labelFor(key: string): string {
		return labelByKey[key]?.() ?? key;
	}

	// Per-row copy: each button copies exactly its own command (never the
	// whole prose blob), then flips to a check for ~1.5s.
	let copiedKey = $state<string | null>(null);
	let resetTimer: ReturnType<typeof setTimeout> | null = null;
	onDestroy(() => {
		if (resetTimer) clearTimeout(resetTimer);
	});

	async function copyCommand(cmd: UpgradeCommand) {
		try {
			await navigator.clipboard.writeText(cmd.command);
			copiedKey = cmd.key;
			if (resetTimer) clearTimeout(resetTimer);
			resetTimer = setTimeout(() => (copiedKey = null), 1500);
		} catch {
			// clipboard unavailable (non-secure context) — tell the user to
			// select the visible command text manually.
			toastStore.error(m.update_clipboard_failed());
		}
	}
</script>

<div class="space-y-1.5" data-upgrade-commands role="group" aria-label={m.update_aria_commands()}>
	{#each commands as cmd (cmd.key)}
		<div
			class="flex items-center gap-2 rounded-md border border-border bg-muted/50 px-2 py-1.5"
			data-upgrade-command={cmd.key}
		>
			<span
				class="w-[4.5rem] shrink-0 truncate text-[10px] font-medium text-muted-foreground"
				title={labelFor(cmd.key)}>{labelFor(cmd.key)}</span
			>
			<!-- Command text may visually truncate in the narrow popover; the
			full string stays on `title` hover and, more importantly, is what the
			copy button writes to the clipboard. -->
			<code class="min-w-0 flex-1 truncate font-mono text-[11px] text-foreground/90" title={cmd.command}
				>{cmd.command}</code
			>
			<button
				type="button"
				onclick={() => copyCommand(cmd)}
				title={m.update_copy_command()}
				aria-label="{m.update_copy()}: {cmd.command}"
				class="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
			>
				{#if copiedKey === cmd.key}
					<Check class="h-3.5 w-3.5 text-emerald-500" />
				{:else}
					<Copy class="h-3.5 w-3.5" />
				{/if}
			</button>
		</div>
	{/each}
</div>