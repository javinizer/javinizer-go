<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
	}

	let { config, inputClass, selectClass }: Props = $props();
</script>

<SettingsSection title={m.settings_database_title()} description={m.settings_database_desc()} defaultExpanded={false}>
	<div class="mb-4">
		<label class="block text-sm font-medium mb-2" for="database-type">{m.settings_database_type_label()}</label>
		<select id="database-type" bind:value={config.database.type} class={selectClass}>
			<option value="sqlite">{m.settings_database_type_sqlite()}</option>
			<option value="postgres">{m.settings_database_type_postgres()}</option>
			<option value="mysql">{m.settings_database_type_mysql()}</option>
		</select>
		<p class="text-xs text-muted-foreground mt-1">
			{m.settings_database_type_desc()}
		</p>
	</div>

	<div class="mb-4">
		<label class="block text-sm font-medium mb-2" for="database-dsn">{m.settings_database_dsn_label()}</label>
		<input
			id="database-dsn"
			type="text"
			bind:value={config.database.dsn}
			class={inputClass}
			placeholder="data/javinizer.db"
		/>
	</div>

	<SettingsSubsection title={m.settings_database_actress_subsection()}>
		<FormToggle
			label={m.settings_database_actress_auto_add_label()}
			description={m.settings_database_actress_auto_add_desc()}
			checked={config.metadata.actress_database?.auto_add ?? false}
			onchange={(val) => {
				if (!config.metadata.actress_database) config.metadata.actress_database = {};
				config.metadata.actress_database.auto_add = val;
			}}
		/>

		<FormToggle
			label={m.settings_database_actress_convert_alias_label()}
			description={m.settings_database_actress_convert_alias_desc()}
			checked={config.metadata.actress_database?.convert_alias ?? false}
			onchange={(val) => {
				if (!config.metadata.actress_database) config.metadata.actress_database = {};
				config.metadata.actress_database.convert_alias = val;
			}}
		/>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_database_tag_subsection()}>
		<FormToggle
			label={m.settings_database_tag_enable_label()}
			description={m.settings_database_tag_enable_desc()}
			checked={config.metadata.tag_database?.enabled ?? false}
			onchange={(val) => {
				if (!config.metadata.tag_database) config.metadata.tag_database = {};
				config.metadata.tag_database.enabled = val;
			}}
		/>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_database_advanced_subsection()}>
		<FormTextInput
			label={m.settings_database_ignore_genres_label()}
			description={m.settings_database_ignore_genres_desc()}
			value={config.metadata.ignore_genres?.join(', ') ?? ''}
			placeholder={m.settings_database_ignore_genres_placeholder()}
			onchange={(val) => {
				config.metadata.ignore_genres = val
					.split(',')
					.map((s) => s.trim())
					.filter((s) => s.length > 0);
			}}
		/>

		<FormTextInput
			label={m.settings_database_required_fields_label()}
			description={m.settings_database_required_fields_desc()}
			value={config.metadata.required_fields?.join(', ') ?? ''}
			placeholder={m.settings_database_required_fields_placeholder()}
			onchange={(val) => {
				config.metadata.required_fields = val
					.split(',')
					.map((s) => s.trim())
					.filter((s) => s.length > 0);
			}}
		/>
	</SettingsSubsection>
</SettingsSection>
