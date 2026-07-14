<script lang="ts">
	import * as m from '$lib/paraglide/messages';
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

<SettingsSection title={m.settings_nfo_title()} description={m.settings_nfo_desc()} defaultExpanded={false}>
	<SettingsSubsection title={m.settings_nfo_basic_subsection()}>
		<FormToggle
			label={m.settings_nfo_enable_label()}
			description={m.settings_nfo_enable_desc()}
			checked={config.metadata.nfo?.enabled ?? true}
			onchange={(val) => {
				if (!config.metadata.nfo) config.metadata.nfo = {};
				config.metadata.nfo.enabled = val;
			}}
		/>

		<fieldset disabled={!nfoEnabled} class={`space-y-0 ${!nfoEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label={m.settings_nfo_per_file_label()}
				description={m.settings_nfo_per_file_desc()}
				checked={config.metadata.nfo?.per_file ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.per_file = val;
				}}
			/>

			<FormTemplateInput
				label={m.settings_nfo_display_title_label()}
				description={m.settings_nfo_display_title_desc()}
				value={config.metadata.nfo?.display_title ?? '[<ID>] <TITLE>'}
				placeholder="[<ID>] <TITLE>"
				showTagList={true}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.display_title = val;
				}}
			/>

			<FormTemplateInput
				label={m.settings_nfo_filename_template_label()}
				description={m.settings_nfo_filename_template_desc()}
				value={config.metadata.nfo?.filename_template ?? '<ID>'}
				placeholder="<ID>"
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.filename_template = val;
				}}
			/>
		</fieldset>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_nfo_actress_subsection()}>
		<fieldset disabled={!nfoEnabled} class={`space-y-0 ${!nfoEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label={m.settings_nfo_first_name_order_label()}
				description={m.settings_nfo_first_name_order_desc()}
				checked={config.metadata.nfo?.first_name_order ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.first_name_order = val;
				}}
			/>

			<FormToggle
				label={m.settings_nfo_japanese_actress_label()}
				description={m.settings_nfo_japanese_actress_desc()}
				checked={config.metadata.nfo?.actress_language_ja ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.actress_language_ja = val;
				}}
			/>

			<FormToggle
				label={m.settings_nfo_unknown_fallback_label()}
				description={m.settings_nfo_unknown_fallback_desc()}
				checked={config.metadata.nfo?.unknown_actress_mode === 'fallback'}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.unknown_actress_mode = val ? 'fallback' : 'skip';
				}}
			/>

			{#if config.metadata.nfo?.unknown_actress_mode === 'fallback'}
			<FormTextInput
				label={m.settings_nfo_unknown_text_label()}
				description={m.settings_nfo_unknown_text_desc()}
				value={config.metadata.nfo?.unknown_actress_text ?? 'Unknown'}
				placeholder={m.settings_nfo_unknown_text_placeholder()}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.unknown_actress_text = val;
				}}
			/>
			{/if}

			<FormToggle
				label={m.settings_nfo_actress_as_tag_label()}
				description={m.settings_nfo_actress_as_tag_desc()}
				checked={config.metadata.nfo?.actress_as_tag ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.actress_as_tag = val;
				}}
			/>

			<FormToggle
				label={m.settings_nfo_generic_role_label()}
				description={m.settings_nfo_generic_role_desc()}
				checked={config.metadata.nfo?.add_generic_role ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.add_generic_role = val;
				}}
			/>

			<FormToggle
				label={m.settings_nfo_alt_name_role_label()}
				description={m.settings_nfo_alt_name_role_desc()}
				checked={config.metadata.nfo?.alt_name_role ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.alt_name_role = val;
				}}
			/>
		</fieldset>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_nfo_media_subsection()}>
		<fieldset disabled={!nfoEnabled} class={`space-y-0 ${!nfoEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label={m.settings_nfo_stream_details_label()}
				description={m.settings_nfo_stream_details_desc()}
				checked={config.metadata.nfo?.include_stream_details ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.include_stream_details = val;
				}}
			/>

			<FormToggle
				label={m.settings_nfo_fanart_label()}
				description={m.settings_nfo_fanart_desc()}
				checked={config.metadata.nfo?.include_fanart ?? true}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.include_fanart = val;
				}}
			/>

			<FormToggle
				label={m.settings_nfo_trailer_label()}
				description={m.settings_nfo_trailer_desc()}
				checked={config.metadata.nfo?.include_trailer ?? true}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.include_trailer = val;
				}}
			/>

			<FormTextInput
				label={m.settings_nfo_rating_source_label()}
				description={m.settings_nfo_rating_source_desc()}
				value={config.metadata.nfo?.rating_source ?? 'r18dev'}
				placeholder="r18dev"
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.rating_source = val;
				}}
			/>
		</fieldset>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_nfo_advanced_subsection()}>
		<fieldset disabled={!nfoEnabled} class={`space-y-0 ${!nfoEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label={m.settings_nfo_original_path_label()}
				description={m.settings_nfo_original_path_desc()}
				checked={config.metadata.nfo?.include_originalpath ?? false}
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.include_originalpath = val;
				}}
			/>

			<FormTemplateInput
				label={m.settings_nfo_tag_template_label()}
				description={m.settings_nfo_tag_template_desc()}
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
				label={m.settings_nfo_tagline_template_label()}
				description={m.settings_nfo_tagline_template_desc()}
				value={config.metadata.nfo?.tagline ?? ''}
				placeholder=""
				onchange={(val) => {
					if (!config.metadata.nfo) config.metadata.nfo = {};
					config.metadata.nfo.tagline = val;
				}}
			/>

			<FormTextInput
				label={m.settings_nfo_credits_label()}
				description={m.settings_nfo_credits_desc()}
				value={config.metadata.nfo?.credits?.join(', ') ?? ''}
				placeholder={m.settings_nfo_credits_placeholder()}
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
