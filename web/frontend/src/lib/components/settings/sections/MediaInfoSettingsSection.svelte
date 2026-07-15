<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import type { SettingsConfig, MediaInfoConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
	}

	let { config }: Props = $props();
	const mediaInfoCliEnabled = $derived(config?.mediainfo?.cli_enabled ?? false);
</script>

<SettingsSection title={m.settings_mediainfo_title()} description={m.settings_mediainfo_desc()} defaultExpanded={false}>
	<div class="space-y-4">
		<FormToggle
			label={m.settings_mediainfo_enable_label()}
			description={m.settings_mediainfo_enable_desc()}
			checked={config.mediainfo?.cli_enabled ?? false}
			onchange={(val) => {
				if (!config.mediainfo) config.mediainfo = {} as MediaInfoConfig;
				config.mediainfo.cli_enabled = val;
			}}
		/>

		<fieldset disabled={!mediaInfoCliEnabled} class={`space-y-0 ${!mediaInfoCliEnabled ? 'opacity-60' : ''}`}>
			<FormTextInput
				label={m.settings_mediainfo_cli_path_label()}
				description={m.settings_mediainfo_cli_path_desc()}
				value={config.mediainfo?.cli_path ?? 'mediainfo'}
				placeholder="mediainfo"
				onchange={(val) => {
					if (!config.mediainfo) config.mediainfo = {} as MediaInfoConfig;
					config.mediainfo.cli_path = val;
				}}
			/>

			<FormNumberInput
				label={m.settings_mediainfo_timeout_label()}
				description={m.settings_mediainfo_timeout_desc()}
				value={config.mediainfo?.cli_timeout ?? 30}
				min={5}
				max={120}
				unit={m.common_unit_seconds()}
				onchange={(val) => {
					if (!config.mediainfo) config.mediainfo = {} as MediaInfoConfig;
					config.mediainfo.cli_timeout = val;
				}}
			/>
		</fieldset>
	</div>
</SettingsSection>
