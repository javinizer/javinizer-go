<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import { FolderOutput, FolderOpen, FileText, FileEdit } from 'lucide-svelte';
	import type { OperationMode, SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
	}

	let { config }: Props = $props();

	let effectiveMode: OperationMode = $derived(
		(config?.output?.operation_mode || 'organize') as OperationMode
	);

	let noFolderFormat: boolean = $derived(
		!config?.output?.folder_format
	);

	function handleOperationModeChange(mode: OperationMode) {
		config.output.operation_mode = mode;
	}
</script>

<SettingsSection title={m.settings_file_ops_title()} description={m.settings_file_ops_desc()} defaultExpanded={false}>
	<div class="space-y-3">
		<h4 class="text-sm font-medium">{m.settings_file_ops_mode_heading()}</h4>
		<p class="text-xs text-muted-foreground">{m.settings_file_ops_mode_desc()}</p>
		<div class="grid grid-cols-2 lg:grid-cols-4 gap-2">
			<button
				onclick={() => handleOperationModeChange('organize')}
				class="flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {effectiveMode === 'organize' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
			>
				<div class="font-medium"><FolderOutput size={16} class="inline mr-1" />{m.settings_file_ops_mode_organize()}</div>
				<div class="text-xs text-muted-foreground">{m.settings_file_ops_mode_organize_desc()}</div>
			</button>

			<button
				onclick={() => handleOperationModeChange('in-place')}
				class="flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {effectiveMode === 'in-place' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
			>
				<div class="font-medium"><FolderOpen size={16} class="inline mr-1" />{m.settings_file_ops_mode_in_place()}</div>
				<div class="text-xs text-muted-foreground">{m.settings_file_ops_mode_in_place_desc()}</div>
			</button>

			<button
				onclick={() => handleOperationModeChange('in-place-norenamefolder')}
				class="flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {effectiveMode === 'in-place-norenamefolder' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
			>
				<div class="font-medium"><FileEdit size={16} class="inline mr-1" />{m.settings_file_ops_mode_rename()}</div>
				<div class="text-xs text-muted-foreground">{m.settings_file_ops_mode_rename_desc()}</div>
			</button>

			<button
				onclick={() => handleOperationModeChange('metadata-artwork')}
				class="flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {effectiveMode === 'metadata-artwork' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
			>
				<div class="font-medium"><FileText size={16} class="inline mr-1" />{m.settings_file_ops_mode_metadata()}</div>
				<div class="text-xs text-muted-foreground">{m.settings_file_ops_mode_metadata_desc()}</div>
			</button>
		</div>
		{#if effectiveMode === 'organize' && noFolderFormat}
			<p class="text-xs text-muted-foreground">
				{m.settings_file_ops_no_folder_format()}
			</p>
		{/if}
	</div>

	<FormToggle
		label={m.settings_file_ops_rename_file_label()}
		description={m.settings_file_ops_rename_file_desc()}
		checked={config.output.rename_file ?? true}
		onchange={(val) => {
			config.output.rename_file = val;
		}}
	/>

	<SettingsSubsection title={m.settings_file_ops_revert_subsection()}>
		<FormToggle
			label={m.settings_file_ops_allow_revert_label()}
			description={m.settings_file_ops_allow_revert_desc()}
			checked={config.output.allow_revert ?? false}
			onchange={(val) => {
				config.output.allow_revert = val;
			}}
		/>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_file_ops_subtitle_subsection()}>
		<FormToggle
			label={m.settings_file_ops_move_subtitles_label()}
			description={m.settings_file_ops_move_subtitles_desc()}
			checked={config.output.move_subtitles ?? false}
			onchange={(val) => {
				config.output.move_subtitles = val;
			}}
		/>

		<FormTextInput
			label={m.settings_file_ops_subtitle_ext_label()}
			description={m.settings_file_ops_subtitle_ext_desc()}
			value={config.output.subtitle_extensions?.join(', ') ?? '.srt, .ass, .ssa, .sub, .vtt'}
			placeholder=".srt, .ass, .ssa, .sub, .vtt"
			onchange={(val) => {
				config.output.subtitle_extensions = val
					.split(',')
					.map((s) => s.trim())
					.filter((s) => s.length > 0);
			}}
		/>
	</SettingsSubsection>
</SettingsSection>