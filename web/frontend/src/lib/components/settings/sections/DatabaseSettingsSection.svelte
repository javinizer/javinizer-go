<script lang="ts">
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	}

	let { config, inputClass }: Props = $props();
</script>

<SettingsSection title="Database Settings" description="Configure database options and behavior" defaultExpanded={false}>
	<div class="mb-4">
		<label class="block text-sm font-medium mb-2" for="database-type">Database Type</label>
		<select id="database-type" bind:value={config.database.type} class={inputClass}>
			<option value="sqlite">SQLite</option>
			<option value="postgres">PostgreSQL</option>
			<option value="mysql">MySQL</option>
		</select>
		<p class="text-xs text-muted-foreground mt-1">
			Database engine to use (SQLite recommended for most users)
		</p>
	</div>

	<div class="mb-4">
		<label class="block text-sm font-medium mb-2" for="database-dsn">Database Path (DSN)</label>
		<input
			id="database-dsn"
			type="text"
			bind:value={config.database.dsn}
			class={inputClass}
			placeholder="data/javinizer.db"
		/>
	</div>

	<SettingsSubsection title="Actress Database">
		<FormToggle
			label="Auto-add actresses"
			description="Automatically add new actresses to the database when encountered"
			checked={config.metadata.actress_database?.auto_add ?? false}
			onchange={(val) => {
				if (!config.metadata.actress_database) config.metadata.actress_database = {};
				config.metadata.actress_database.auto_add = val;
			}}
		/>

		<FormToggle
			label="Convert aliases"
			description="Use actress aliases from the database when generating metadata"
			checked={config.metadata.actress_database?.convert_alias ?? false}
			onchange={(val) => {
				if (!config.metadata.actress_database) config.metadata.actress_database = {};
				config.metadata.actress_database.convert_alias = val;
			}}
		/>
	</SettingsSubsection>

	<SettingsSubsection title="Genre Replacement">
		<FormToggle
			label="Auto-add genres"
			description="Automatically add new genre replacements to the database"
			checked={config.metadata.genre_replacement?.auto_add ?? false}
			onchange={(val) => {
				if (!config.metadata.genre_replacement) config.metadata.genre_replacement = {};
				config.metadata.genre_replacement.auto_add = val;
			}}
		/>
	</SettingsSubsection>

	<SettingsSubsection title="Tag Database">
		<FormToggle
			label="Enable tag database"
			description="Enable per-movie tag lookup from database"
			checked={config.metadata.tag_database?.enabled ?? false}
			onchange={(val) => {
				if (!config.metadata.tag_database) config.metadata.tag_database = {};
				config.metadata.tag_database.enabled = val;
			}}
		/>
	</SettingsSubsection>

	<SettingsSubsection title="Advanced Metadata Options">
		<FormTextInput
			label="Ignore genres"
			description="Comma-separated list of genres to exclude from metadata"
			value={config.metadata.ignore_genres?.join(', ') ?? ''}
			placeholder="e.g., Sample, Trailer"
			onchange={(val) => {
				config.metadata.ignore_genres = val
					.split(',')
					.map((s) => s.trim())
					.filter((s) => s.length > 0);
			}}
		/>

		<FormTextInput
			label="Required fields"
			description="Comma-separated list of required metadata fields (scraping fails if missing)"
			value={config.metadata.required_fields?.join(', ') ?? ''}
			placeholder="e.g., title, actress, studio"
			onchange={(val) => {
				config.metadata.required_fields = val
					.split(',')
					.map((s) => s.trim())
					.filter((s) => s.length > 0);
			}}
		/>
	</SettingsSubsection>
</SettingsSection>
