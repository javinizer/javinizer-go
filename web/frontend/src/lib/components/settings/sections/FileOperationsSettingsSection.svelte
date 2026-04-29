<script lang="ts">
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

<SettingsSection title="File Operations" description="Control how Javinizer organizes and moves your files" defaultExpanded={false}>
	<div class="space-y-3">
		<h4 class="text-sm font-medium">Operation Mode</h4>
		<p class="text-xs text-muted-foreground">Choose how files are organized during operations</p>
		<div class="grid grid-cols-2 lg:grid-cols-4 gap-2">
			<button
				onclick={() => handleOperationModeChange('organize')}
				class="flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {effectiveMode === 'organize' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
			>
				<div class="font-medium"><FolderOutput size={16} class="inline mr-1" />Organize</div>
				<div class="text-xs text-muted-foreground">Move to organized folder structure</div>
			</button>

			<button
				onclick={() => handleOperationModeChange('in-place')}
				class="flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {effectiveMode === 'in-place' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
			>
				<div class="font-medium"><FolderOpen size={16} class="inline mr-1" />Reorganize in place</div>
				<div class="text-xs text-muted-foreground">Keep location, rename folder and file</div>
			</button>

			<button
				onclick={() => handleOperationModeChange('in-place-norenamefolder')}
				class="flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {effectiveMode === 'in-place-norenamefolder' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
			>
				<div class="font-medium"><FileEdit size={16} class="inline mr-1" />Rename file only</div>
				<div class="text-xs text-muted-foreground">Rename video file, keep folder name</div>
			</button>

			<button
				onclick={() => handleOperationModeChange('metadata-only')}
				class="flex flex-col items-start gap-1 p-3 rounded-lg border-2 text-sm transition-all {effectiveMode === 'metadata-only' ? 'border-primary bg-primary/5 font-medium' : 'border-border hover:border-primary/50'}"
			>
				<div class="font-medium"><FileText size={16} class="inline mr-1" />Metadata only</div>
				<div class="text-xs text-muted-foreground">No file or folder changes</div>
			</button>
		</div>
		{#if effectiveMode === 'organize' && noFolderFormat}
			<p class="text-xs text-muted-foreground">
				No folder naming template set — when destination matches source path, files will be renamed in place only.
			</p>
		{/if}
	</div>

	<FormToggle
		label="Rename file"
		description="Rename video files according to the file naming template"
		checked={config.output.rename_file ?? true}
		onchange={(val) => {
			config.output.rename_file = val;
		}}
	/>

	<SettingsSubsection title="Revert">
		<FormToggle
			label="Allow Revert"
			description="Enable the revert feature to undo organize operations and restore files to their original locations. When disabled, revert buttons are hidden and revert API calls are blocked."
			checked={config.output.allow_revert ?? false}
			onchange={(val) => {
				config.output.allow_revert = val;
			}}
		/>
	</SettingsSubsection>

	<SettingsSubsection title="Subtitle Handling">
		<FormToggle
			label="Move subtitles"
			description="Automatically move subtitle files (.srt, .ass, etc.) with video files"
			checked={config.output.move_subtitles ?? false}
			onchange={(val) => {
				config.output.move_subtitles = val;
			}}
		/>

		<FormTextInput
			label="Subtitle extensions"
			description="Comma-separated list of subtitle file extensions to move with videos"
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