<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import { apiClient } from '$lib/api/client';
	import { toastStore } from '$lib/stores/toast';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import AllowedDirectoriesEditor from '$lib/components/AllowedDirectoriesEditor.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { FolderPlus, Trash2, Save } from 'lucide-svelte';
	import type { Config, SettingsConfig, SecurityUpdateRequest } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
	}

	let { config, inputClass, selectClass }: Props = $props();

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
	// baseline tracks the last saved/loaded security state so `dirty` stays
	// accurate even while the sync effect (below) mirrors draft edits into the
	// shared config object. Populated by the rehydrate $effect on mount.
	let baseline = $state<SecurityDraft>({
		allowed_directories: [],
		denied_directories: [],
		allow_unc: false,
		allowed_unc_servers: [],
	});

	$effect(() => {
		const cfg = hydratedConfig ?? config;
		if (cfg !== lastConfigRef) {
			lastConfigRef = cfg;
			const snap = snapshot(cfg);
			draft = snap;
			baseline = snapshot(cfg);
		}
	});

	// Mirror draft edits into the parent config object so the top-level
	// "Save Changes" button (which PUTs the whole config) persists security
	// edits too. Without this, only the narrow "Save Security" endpoint would
	// see the draft, and a whole-config save would clobber the directories the
	// user just added with the stale value still held in settings.config.
	$effect(() => {
		const sec = config?.api?.security;
		if (!sec) return;
		sec.allowed_directories = [...draft.allowed_directories];
		sec.denied_directories = [...draft.denied_directories];
		sec.allow_unc = draft.allow_unc;
		sec.allowed_unc_servers = [...draft.allowed_unc_servers];
	});

	let newUncServer = $state('');

	let dirty = $derived(
		!arrayEqual(draft.allowed_directories, baseline.allowed_directories) ||
			!arrayEqual(draft.denied_directories, baseline.denied_directories) ||
			draft.allow_unc !== baseline.allow_unc ||
			!arrayEqual(draft.allowed_unc_servers, baseline.allowed_unc_servers),
	);

	function arrayEqual(a: string[], b: string[]): boolean {
		if (a.length !== b.length) return false;
		for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
		return true;
	}

	function addUncServer() {
		const server = newUncServer.trim();
		if (!server) return;
		if (draft.allowed_unc_servers.includes(server)) {
			toastStore.error(m.settings_security_unc_already_list(), 3000);
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
			const saved: SecurityDraft = {
				allowed_directories: [...(data.security.allowed_directories ?? [])],
				denied_directories: [...(data.security.denied_directories ?? [])],
				allow_unc: data.security.allow_unc,
				allowed_unc_servers: [...(data.security.allowed_unc_servers ?? [])],
			};
			draft = saved;
			baseline = {
				allowed_directories: [...saved.allowed_directories],
				denied_directories: [...saved.denied_directories],
				allow_unc: saved.allow_unc,
				allowed_unc_servers: [...saved.allowed_unc_servers],
			};
			toastStore.success(m.settings_security_saved_toast(), 4000);
			void queryClient.invalidateQueries({ queryKey: ['config'] }).then(async () => {
				const fresh = await queryClient.fetchQuery<Config>({
					queryKey: ['config'],
					queryFn: () => apiClient.getConfig(),
				});
				hydratedConfig = fresh as unknown as SettingsConfig;
			});
		},
		onError: (err: Error) => {
			toastStore.error(err.message || m.settings_security_save_failed(), 5000);
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
	title={m.settings_security_title()}
	description={m.settings_security_desc()}
	defaultExpanded={false}
>
	<SettingsSubsection title={m.settings_security_allowed_subsection()}>
		<p class="text-xs text-muted-foreground mb-3">
			{m.settings_security_allowed_desc()}
		</p>

		<AllowedDirectoriesEditor
			bind:directories={draft.allowed_directories}
			whitelistPaths={draft.allowed_directories}
		/>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_security_denied_subsection()}>
		<p class="text-xs text-muted-foreground mb-3">
			{m.settings_security_denied_desc()}
		</p>

		<AllowedDirectoriesEditor
			bind:directories={draft.denied_directories}
			whitelistPaths={draft.allowed_directories}
			showDefaultBadge={false}
			placeholder={m.settings_security_deny_placeholder()}
			emptyHint="No denied directories. Add one below to block specific paths even within allowed directories."
		/>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_security_unc_subsection()}>
		<FormToggle
			id="security-allow-unc"
			label={m.settings_security_allow_unc_label()}
			description={m.settings_security_allow_unc_desc()}
			checked={draft.allow_unc}
			onchange={(val) => { draft.allow_unc = val; }}
		/>

		{#if draft.allow_unc}
			<div class="mt-3">
				<label class="block text-sm font-medium mb-2" for="security-unc-servers">{m.settings_security_unc_servers_label()}</label>
				{#if draft.allowed_unc_servers.length > 0}
					<ul class="space-y-2 mb-3">
						{#each draft.allowed_unc_servers as server, index (server + '-' + index)}
							<li class="flex items-center gap-2 rounded-md border border-border bg-background px-3 py-2">
								<span class="flex-1 min-w-0 truncate font-mono text-sm">{server}</span>
								<button
									type="button"
									class="text-muted-foreground hover:text-destructive transition-colors shrink-0"
									title={m.settings_security_remove_server_tooltip()}
									aria-label={m.settings_security_remove_unc_aria({ server })}
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
					<Button variant="outline" size="sm" onclick={addUncServer} disabled={!newUncServer.trim()} title={m.settings_security_add_unc_tooltip()}>
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
				{saveMutation.isPending ? m.common_saving() : m.settings_security_save_button()}
			{/snippet}
		</Button>
	</div>
</SettingsSection>
