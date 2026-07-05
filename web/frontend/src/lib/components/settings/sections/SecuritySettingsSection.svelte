<script lang="ts">
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import PathInput from '$lib/components/PathInput.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { FolderPlus, Trash2, Save, Star, Folder } from 'lucide-svelte';
	import type { Config, SettingsConfig, SecurityUpdateRequest } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	}

	let { config, inputClass }: Props = $props();

	const queryClient = useQueryClient();

	type SecurityDraft = {
		allowed_directories: string[];
		denied_directories: string[];
		allow_unc: boolean;
		allowed_unc_servers: string[];
	};

	function snapshot(cfg: SettingsConfig): SecurityDraft {
		const sec = cfg?.api?.security;
		return {
			allowed_directories: [...(sec?.allowed_directories ?? [])],
			denied_directories: [...(sec?.denied_directories ?? [])],
			allow_unc: sec?.allow_unc ?? false,
			allowed_unc_servers: [...(sec?.allowed_unc_servers ?? [])],
		};
	}

	let hydratedConfig = $state<SettingsConfig | null>(null);
	let lastConfigRef: SettingsConfig | null = null;
	let draft = $state<SecurityDraft>({
		allowed_directories: [],
		denied_directories: [],
		allow_unc: false,
		allowed_unc_servers: [],
	});

	$effect(() => {
		const cfg = hydratedConfig ?? config;
		if (cfg !== lastConfigRef) {
			lastConfigRef = cfg;
			draft = snapshot(cfg);
		}
	});

	let newAllowedDir = $state('');
	let newDeniedDir = $state('');
	let newUncServer = $state('');

	let dirty = $derived(
		(() => {
			const sec = (hydratedConfig ?? config)?.api?.security;
			return (
				!arrayEqual(draft.allowed_directories, sec?.allowed_directories ?? []) ||
				!arrayEqual(draft.denied_directories, sec?.denied_directories ?? []) ||
				draft.allow_unc !== (sec?.allow_unc ?? false) ||
				!arrayEqual(draft.allowed_unc_servers, sec?.allowed_unc_servers ?? [])
			);
		})(),
	);

	function arrayEqual(a: string[], b: string[]): boolean {
		if (a.length !== b.length) return false;
		for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
		return true;
	}

	function addAllowedDir() {
		const path = newAllowedDir.trim();
		if (!path) return;
		if (draft.allowed_directories.includes(path)) {
			toastStore.error('Directory already in the allowed list', 3000);
			return;
		}
		draft.allowed_directories = [...draft.allowed_directories, path];
		newAllowedDir = '';
	}

	function removeAllowedDir(index: number) {
		draft.allowed_directories = draft.allowed_directories.filter((_, i) => i !== index);
	}

	function addDeniedDir() {
		const path = newDeniedDir.trim();
		if (!path) return;
		if (draft.denied_directories.includes(path)) {
			toastStore.error('Directory already in the denied list', 3000);
			return;
		}
		draft.denied_directories = [...draft.denied_directories, path];
		newDeniedDir = '';
	}

	function removeDeniedDir(index: number) {
		draft.denied_directories = draft.denied_directories.filter((_, i) => i !== index);
	}

	function addUncServer() {
		const server = newUncServer.trim();
		if (!server) return;
		if (draft.allowed_unc_servers.includes(server)) {
			toastStore.error('Server already in the allow list', 3000);
			return;
		}
		draft.allowed_unc_servers = [...draft.allowed_unc_servers, server];
		newUncServer = '';
	}

	function removeUncServer(index: number) {
		draft.allowed_unc_servers = draft.allowed_unc_servers.filter((_, i) => i !== index);
	}

	const saveMutation = createMutation(() => ({
		mutationFn: async (req: SecurityUpdateRequest) => {
			return apiClient.updateSecurityConfig(req);
		},
		onSuccess: (data) => {
			draft = {
				allowed_directories: [...(data.security.allowed_directories ?? [])],
				denied_directories: [...(data.security.denied_directories ?? [])],
				allow_unc: data.security.allow_unc,
				allowed_unc_servers: [...(data.security.allowed_unc_servers ?? [])],
			};
			toastStore.success('Security settings saved and reloaded', 4000);
			void queryClient.invalidateQueries({ queryKey: ['config'] }).then(async () => {
				const fresh = await queryClient.fetchQuery<Config>({
					queryKey: ['config'],
					queryFn: () => apiClient.getConfig(),
				});
				hydratedConfig = fresh as unknown as SettingsConfig;
			});
		},
		onError: (err: Error) => {
			toastStore.error(err.message || 'Failed to save security settings', 5000);
		},
	}));

	function handleSave() {
		saveMutation.mutate({
			allowed_directories: draft.allowed_directories,
			denied_directories: draft.denied_directories,
			allow_unc: draft.allow_unc,
			allowed_unc_servers: draft.allowed_unc_servers,
		});
	}
</script>

<SettingsSection
	title="Security / Allowed Directories"
	description="Control which directories Javinizer can scan and write to. The first allowed directory is the default scan path. Changes are saved and hot-reloaded immediately."
	defaultExpanded={false}
>
	<SettingsSubsection title="Allowed Directories">
		<p class="text-xs text-muted-foreground mb-3">
			Javinizer will only scan and operate inside these directories. With no allowed directories configured, all file operations are blocked.
		</p>

		{#if draft.allowed_directories.length === 0}
			<div class="rounded-lg border border-dashed border-border p-4 text-center text-sm text-muted-foreground">
				No allowed directories configured. Add one below to enable scanning.
			</div>
		{:else}
			<ul class="space-y-2 mb-3">
				{#each draft.allowed_directories as dir, index (dir + '-' + index)}
					<li class="flex items-center gap-2 rounded-md border border-border bg-background px-3 py-2">
						{#if index === 0}
							<span
								class="inline-flex items-center gap-1 rounded bg-amber-500/15 text-amber-700 dark:text-amber-400 text-xs font-medium px-2 py-0.5 shrink-0"
								title="The first allowed directory is used as the default scan path"
							>
								<Star class="h-3 w-3" />
								Default
							</span>
						{:else}
							<Folder class="h-4 w-4 text-muted-foreground shrink-0" />
						{/if}
						<span class="flex-1 min-w-0 truncate font-mono text-sm">{dir}</span>
						<button
							type="button"
							class="text-muted-foreground hover:text-destructive transition-colors shrink-0"
							title="Remove directory"
							aria-label="Remove allowed directory {dir}"
							onclick={() => removeAllowedDir(index)}
						>
							<Trash2 class="h-4 w-4" />
						</button>
					</li>
				{/each}
			</ul>
		{/if}

		<div class="flex items-start gap-2">
			<PathInput
				bind:value={newAllowedDir}
				placeholder="Add a directory (e.g. /mnt/videos)"
				whitelistPaths={draft.allowed_directories}
				class="flex-1"
			/>
			<Button variant="outline" size="sm" onclick={addAllowedDir} disabled={!newAllowedDir.trim()} title="Add allowed directory">
				{#snippet children()}
					<FolderPlus class="h-4 w-4" />
				{/snippet}
			</Button>
		</div>
		<p class="text-xs text-muted-foreground mt-1">
			Autocomplete uses the current allowed directories. Type the first path manually if the list is empty.
		</p>
	</SettingsSubsection>

	<SettingsSubsection title="Denied Directories">
		<p class="text-xs text-muted-foreground mb-3">
			Paths here are always blocked, even if they fall under an allowed directory. Built-in system directories (/proc, /sys, /dev) are always denied.
		</p>

		{#if draft.denied_directories.length > 0}
			<ul class="space-y-2 mb-3">
				{#each draft.denied_directories as dir, index (dir + '-' + index)}
					<li class="flex items-center gap-2 rounded-md border border-border bg-background px-3 py-2">
						<Folder class="h-4 w-4 text-muted-foreground shrink-0" />
						<span class="flex-1 min-w-0 truncate font-mono text-sm">{dir}</span>
						<button
							type="button"
							class="text-muted-foreground hover:text-destructive transition-colors shrink-0"
							title="Remove directory"
							aria-label="Remove denied directory {dir}"
							onclick={() => removeDeniedDir(index)}
						>
							<Trash2 class="h-4 w-4" />
						</button>
					</li>
				{/each}
			</ul>
		{/if}

		<div class="flex items-start gap-2">
			<PathInput
				bind:value={newDeniedDir}
				placeholder="Add a directory to deny (e.g. /sensitive)"
				whitelistPaths={draft.allowed_directories}
				class="flex-1"
			/>
			<Button variant="outline" size="sm" onclick={addDeniedDir} disabled={!newDeniedDir.trim()} title="Add denied directory">
				{#snippet children()}
					<FolderPlus class="h-4 w-4" />
				{/snippet}
			</Button>
		</div>
	</SettingsSubsection>

	<SettingsSubsection title="UNC / Network Paths (Windows)">
		<FormToggle
			id="security-allow-unc"
			label="Allow UNC paths"
			description="Permit \\\\server\\share paths. UNC paths can leak NTLM credentials; enable only with trusted servers."
			checked={draft.allow_unc}
			onchange={(val) => { draft.allow_unc = val; }}
		/>

		{#if draft.allow_unc}
			<div class="mt-3">
				<label class="block text-sm font-medium mb-2" for="security-unc-servers">Allowed UNC servers</label>
				{#if draft.allowed_unc_servers.length > 0}
					<ul class="space-y-2 mb-3">
						{#each draft.allowed_unc_servers as server, index (server + '-' + index)}
							<li class="flex items-center gap-2 rounded-md border border-border bg-background px-3 py-2">
								<span class="flex-1 min-w-0 truncate font-mono text-sm">{server}</span>
								<button
									type="button"
									class="text-muted-foreground hover:text-destructive transition-colors shrink-0"
									title="Remove server"
									aria-label="Remove UNC server {server}"
									onclick={() => removeUncServer(index)}
								>
									<Trash2 class="h-4 w-4" />
								</button>
							</li>
						{/each}
					</ul>
				{/if}
				<div class="flex items-start gap-2">
					<input
						id="security-unc-servers"
						type="text"
						bind:value={newUncServer}
						onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addUncServer(); } }}
						placeholder="\\server\share"
						class="{inputClass} flex-1 font-mono text-sm"
					/>
					<Button variant="outline" size="sm" onclick={addUncServer} disabled={!newUncServer.trim()} title="Add UNC server">
						{#snippet children()}
							<FolderPlus class="h-4 w-4" />
						{/snippet}
					</Button>
				</div>
			</div>
		{/if}
	</SettingsSubsection>

	<div class="flex items-center justify-end gap-2 pt-4 border-t border-border">
		<Button onclick={handleSave} disabled={!dirty || saveMutation.isPending}>
			{#snippet children()}
				<Save class="h-4 w-4 mr-2" />
				{saveMutation.isPending ? 'Saving...' : 'Save Security'}
			{/snippet}
		</Button>
	</div>
</SettingsSection>
