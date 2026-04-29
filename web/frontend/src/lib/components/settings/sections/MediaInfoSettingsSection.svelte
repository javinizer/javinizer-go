<script lang="ts">
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

<SettingsSection title="MediaInfo Settings" description="Configure MediaInfo CLI fallback for media file analysis" defaultExpanded={false}>
	<div class="space-y-4">
		<FormToggle
			label="Enable MediaInfo CLI"
			description="Enable MediaInfo CLI fallback when library-based parsing fails"
			checked={config.mediainfo?.cli_enabled ?? false}
			onchange={(val) => {
				if (!config.mediainfo) config.mediainfo = {} as MediaInfoConfig;
				config.mediainfo.cli_enabled = val;
			}}
		/>

		<fieldset disabled={!mediaInfoCliEnabled} class={`space-y-0 ${!mediaInfoCliEnabled ? 'opacity-60' : ''}`}>
			<FormTextInput
				label="MediaInfo CLI path"
				description="Path to the mediainfo binary (default: 'mediainfo' from PATH)"
				value={config.mediainfo?.cli_path ?? 'mediainfo'}
				placeholder="mediainfo"
				onchange={(val) => {
					if (!config.mediainfo) config.mediainfo = {} as MediaInfoConfig;
					config.mediainfo.cli_path = val;
				}}
			/>

			<FormNumberInput
				label="CLI timeout"
				description="Maximum time to wait for MediaInfo CLI execution"
				value={config.mediainfo?.cli_timeout ?? 30}
				min={5}
				max={120}
				unit="seconds"
				onchange={(val) => {
					if (!config.mediainfo) config.mediainfo = {} as MediaInfoConfig;
					config.mediainfo.cli_timeout = val;
				}}
			/>
		</fieldset>
	</div>
</SettingsSection>
