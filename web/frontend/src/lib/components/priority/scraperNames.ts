/**
 * Display labels for scraper config keys.
 *
 * Used by the Metadata Priority UI (MetadataPriority.svelte + FieldRow.svelte)
 * so a scraper chip renders "MGStage" rather than the raw `mgstage` config key.
 * Kept in one place so the table can't drift between the two call sites
 * (issue #105: `mgstage`/`fc2`/`javstash` were missing and rendered verbatim).
 */
export function formatScraperName(name: string): string {
	switch (name) {
		case 'dmm':
			return 'DMM/Fanza';
		case 'libredmm':
			return 'LibreDMM (Fanza, MGStage, SOD, FC2)';
		case 'r18dev':
			return 'R18.dev';
		case 'javlibrary':
			return 'JavLibrary';
		case 'javdb':
			return 'JavDB';
		case 'javbus':
			return 'JavBus';
		case 'jav321':
			return 'Jav321';
		case 'tokyohot':
			return 'Tokyo-Hot';
		case 'aventertainment':
			return 'AV Entertainment';
		case 'dlgetchu':
			return 'DLGetchu';
		case 'caribbeancom':
			return 'Caribbeancom';
		case 'mgstage':
			return 'MGStage';
		case 'fc2':
			return 'FC2';
		case 'javstash':
			return 'JAVStash';
		default:
			return name;
	}
}
