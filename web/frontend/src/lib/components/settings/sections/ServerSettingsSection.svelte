<script lang="ts">
	import { onMount } from 'svelte';
	import { RefreshCw, ArrowUpCircle } from 'lucide-svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import type { VersionStatusResponse, SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	}

	let { config, inputClass }: Props = $props();

	let versionStatus = $state<VersionStatusResponse | null>(null);
	let isCheckingVersion = $state(false);

	async function loadVersionStatus() {
		try {
			versionStatus = await apiClient.getVersionStatus();
		} catch {
			versionStatus = null;
		}
	}

	async function checkVersion() {
		isCheckingVersion = true;
		try {
			versionStatus = await apiClient.checkVersion();
			if (versionStatus.error) {
				toastStore.error(`Version check failed: ${versionStatus.error}`);
			} else if (versionStatus.update_available) {
				toastStore.info(`Update available: v${versionStatus.latest}`);
			} else {
				toastStore.success('You are on the latest version');
			}
		} catch (e) {
			toastStore.error(`Version check failed: ${e instanceof Error ? e.message : 'Unknown error'}`);
		} finally {
			isCheckingVersion = false;
		}
	}

	function sanitizeTempDir(value: string): string {
		value = value.trim();
		value = value.replace(/\.\.[\\/]/g, '');
		value = value.replace(/^[\\/]+|[\\/]+$/g, '');
		return value;
	}

	function handleTempDirInput(e: Event) {
		const target = e.target as HTMLInputElement;
		config.system.temp_dir = sanitizeTempDir(target.value);
	}

	onMount(() => {
		loadVersionStatus();
	});
</script>

<SettingsSection title="Server Settings" description="Configure API server host, port, and system paths" defaultExpanded={false}>
	<div class="space-y-4">
		<div class="p-3 bg-muted/30 rounded-lg border border-border">
			<div class="flex items-center justify-between mb-3">
				<div class="flex items-center gap-2">
					<span class="text-sm font-medium">Version</span>
					{#if versionStatus}
						<span class="text-sm text-muted-foreground">{versionStatus.current}</span>
						{#if versionStatus.update_available}
							<span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-green-500/10 text-green-600">
								<ArrowUpCircle class="h-3 w-3" />
								Update available: {versionStatus.latest}
							</span>
						{/if}
					{:else}
						<span class="text-sm text-muted-foreground">—</span>
					{/if}
				</div>
				<button
					type="button"
					onclick={checkVersion}
					disabled={isCheckingVersion}
					class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md border border-input bg-background text-sm hover:bg-accent hover:text-accent-foreground disabled:opacity-50 transition-colors"
				>
					<RefreshCw class="h-3.5 w-3.5 {isCheckingVersion ? 'animate-spin' : ''}" />
					{isCheckingVersion ? 'Checking...' : 'Check for Updates'}
				</button>
			</div>
			<div class="space-y-3">
				<div class="flex items-center gap-2">
					<input
						id="version-check-enabled"
						type="checkbox"
						bind:checked={config.system.version_check_enabled}
						class="w-4 h-4"
					/>
					<label class="text-sm font-medium" for="version-check-enabled">Enable automatic version checking</label>
				</div>
				{#if config.system.version_check_enabled}
					<div>
						<label class="block text-sm font-medium mb-2" for="version-check-interval">Check Interval (hours)</label>
						<input
							id="version-check-interval"
							type="number"
							bind:value={config.system.version_check_interval_hours}
							class={inputClass}
							min="1"
							placeholder="24"
						/>
					</div>
				{/if}
			</div>
		</div>

		<div class="grid grid-cols-2 gap-4">
			<div>
				<label class="block text-sm font-medium mb-2" for="server-host">Host</label>
				<input id="server-host" type="text" bind:value={config.server.host} class={inputClass} placeholder="localhost" />
			</div>
			<div>
				<label class="block text-sm font-medium mb-2" for="server-port">Port</label>
				<input id="server-port" type="number" bind:value={config.server.port} class={inputClass} placeholder="8080" />
			</div>
		</div>
		<div>
			<label class="block text-sm font-medium mb-2" for="system-temp-dir">Temporary Directory</label>
			<input
				id="system-temp-dir"
				type="text"
				value={config.system.temp_dir}
				oninput={handleTempDirInput}
				class={inputClass}
				placeholder="data/temp"
			/>
			<p class="text-xs text-muted-foreground mt-1">Base directory for temporary files (default: data/temp). Cannot contain path traversal patterns.</p>
		</div>
	</div>
</SettingsSection>
