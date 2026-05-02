<script lang="ts">
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	}

	let { config, inputClass }: Props = $props();

	function getSelectedView(): string {
		return config.webui?.default_review_view || 'grid-poster';
	}

	function setSelectedView(value: string) {
		if (!config.webui) config.webui = {};
		config.webui.default_review_view = value;
	}
</script>

<SettingsSection title="Web UI" description="Configure web interface preferences" defaultExpanded={false}>
	<div class="space-y-4">
		<div>
			<label class="block text-sm font-medium mb-2" for="webui-default-review-view">Default Review View</label>
			<select
				id="webui-default-review-view"
				value={getSelectedView()}
				onchange={(e) => setSelectedView((e.target as HTMLSelectElement).value)}
				class={inputClass}
			>
				<option value="grid-poster">Grid (Poster)</option>
				<option value="grid-cover">Grid (Cover)</option>
				<option value="detail">Detail</option>
			</select>
			<p class="text-xs text-muted-foreground mt-1">
				Default view mode when opening the review page. Users can still switch views at runtime.
			</p>
		</div>
	</div>
</SettingsSection>
