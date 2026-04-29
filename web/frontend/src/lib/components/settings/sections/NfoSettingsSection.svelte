<script lang="ts">
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormTemplateInput from '$lib/components/settings/FormTemplateInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
	}

	let { config }: Props = $props();
	const nfoEnabled = $derived(config?.metadata?.nfo?.enabled ?? true);
</script>

<SettingsSection title="NFO Settings" description="Configure NFO metadata file generation for Kodi/Plex compatibility" defaultExpanded={false}>
	<SettingsSubsection title="Basic NFO Options">
		<FormToggle
			label="Enable NFO generation"
			description="Generate .nfo metadata files for use with media servers like Kodi and Plex"
			checked={config.metadata.nfo?.enabled ?? true}
			onchange={(val) => {
				if (!config.metadata.nfo) config.metadata.nfo = {};
				config.metadata.nfo.enabled = val;
			}}
		/>

		<fieldset disabled={!nfoEnabled} class={`space-y-0 ${!nfoEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label="NFO per file"
				description="Create separate NFO files for each video file (useful for multi-part movies)"
				checked={config.metadata.nfo?.per_file ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.per_file = val;
				}}
			/>

			<FormTemplateInput
				label="Display title template"
				description="Template for the <title> field in NFO files"
				value={config.metadata.nfo?.display_title ?? '[<ID>] <TITLE>'}
				placeholder="[<ID>] <TITLE>"
				showTagList={true}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.display_title = val;
				}}
			/>

			<FormTemplateInput
				label="Filename template"
				description="Template for NFO filenames"
				value={config.metadata.nfo?.filename_template ?? '<ID>'}
				placeholder="<ID>"
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.filename_template = val;
				}}
			/>
		</fieldset>
	</SettingsSubsection>

	<SettingsSubsection title="Actress Settings">
		<fieldset disabled={!nfoEnabled} class={`space-y-0 ${!nfoEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label="First name order"
				description="Use first-name-first order for actress names (Western style)"
				checked={config.metadata.nfo?.first_name_order ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.first_name_order = val;
				}}
			/>

			<FormToggle
				label="Japanese actress names"
				description="Use Japanese names for actresses in NFO files"
				checked={config.metadata.nfo?.actress_language_ja ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.actress_language_ja = val;
				}}
			/>

			<FormToggle
				label="Unknown actress fallback"
				description="Show placeholder text when no actress information is available (off = omit entirely)"
				checked={config.metadata.nfo?.unknown_actress_mode === 'fallback'}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.unknown_actress_mode = val ? 'fallback' : 'skip';
				}}
			/>

			{#if config.metadata.nfo?.unknown_actress_mode === 'fallback'}
			<FormTextInput
				label="Unknown actress text"
				description="Text to display when actress information is unavailable"
				value={config.metadata.nfo?.unknown_actress_text ?? 'Unknown'}
				placeholder="Unknown"
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.unknown_actress_text = val;
				}}
			/>
			{/if}

			<FormToggle
				label="Actress as tag"
				description="Include actress names in the <tag> field"
				checked={config.metadata.nfo?.actress_as_tag ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.actress_as_tag = val;
				}}
			/>

			<FormToggle
				label="Add generic role"
				description="Add 'Actress' as a generic role for all performers"
				checked={config.metadata.nfo?.add_generic_role ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.add_generic_role = val;
				}}
			/>

			<FormToggle
				label="Use alternate name for role"
				description="Use actress alternate names in <role> field"
				checked={config.metadata.nfo?.alt_name_role ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.alt_name_role = val;
				}}
			/>
		</fieldset>
	</SettingsSubsection>

	<SettingsSubsection title="Media Information">
		<fieldset disabled={!nfoEnabled} class={`space-y-0 ${!nfoEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label="Include stream details"
				description="Include video/audio codec information from MediaInfo analysis"
				checked={config.metadata.nfo?.include_stream_details ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.include_stream_details = val;
				}}
			/>

			<FormToggle
				label="Include fanart"
				description="Include fanart/cover image reference in NFO"
				checked={config.metadata.nfo?.include_fanart ?? true}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.include_fanart = val;
				}}
			/>

			<FormToggle
				label="Include trailer"
				description="Include trailer video reference in NFO"
				checked={config.metadata.nfo?.include_trailer ?? true}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.include_trailer = val;
				}}
			/>

			<FormTextInput
				label="Rating source"
				description="Source name for movie ratings (e.g., 'r18dev', 'dmm')"
				value={config.metadata.nfo?.rating_source ?? 'r18dev'}
				placeholder="r18dev"
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.rating_source = val;
				}}
			/>
		</fieldset>
	</SettingsSubsection>

	<SettingsSubsection title="Advanced NFO Options">
		<fieldset disabled={!nfoEnabled} class={`space-y-0 ${!nfoEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label="Include original path"
				description="Include the original file path in NFO metadata"
				checked={config.metadata.nfo?.include_originalpath ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.include_originalpath = val;
				}}
			/>

			<FormTemplateInput
				label="Tag template"
				description="Template for custom tags in NFO files"
				value={(Array.isArray(config.metadata.nfo?.tag) ? config.metadata.nfo.tag.join(', ') : config.metadata.nfo?.tag) ?? '<SET>'}
				placeholder="<SET>"
				showTagList={true}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.tag = val
						.split(',')
						.map((s) => s.trim())
						.filter((s) => s.length > 0);
				}}
			/>

			<FormTemplateInput
				label="Tagline template"
				description="Template for the tagline field in NFO files"
				value={config.metadata.nfo?.tagline ?? ''}
				placeholder=""
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.tagline = val;
				}}
			/>

			<FormTextInput
				label="Credits"
				description="Credits to include in NFO (comma-separated)"
				value={config.metadata.nfo?.credits?.join(', ') ?? ''}
				placeholder="Director Name, Studio Name"
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.credits = val
						.split(',')
						.map((s) => s.trim())
						.filter((s) => s.length > 0);
				}}
			/>
		</fieldset>
	</SettingsSubsection>
</SettingsSection>
