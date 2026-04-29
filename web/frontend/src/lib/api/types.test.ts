import { describe, it, expect, assertType } from 'vitest';
import type {
	FieldDifference,
	ActressMergeConflict,
	ScraperOption,
	ProxyTestRequest,
	ScrapersConfig,
	Config,
	ProxyConfig,
	FlareSolverrConfig,
	ServerConfig,
	APIConfig,
	SystemConfig,
	MetadataConfig,
	MatchingConfig,
	OutputConfig,
	DatabaseConfig,
	MediaInfoConfig,
	ScraperSettings,
	NFOConfig,
	TranslationConfig,
	TranslationFieldsConfig,
	ProxyProfile
} from './types';

describe('types.ts has no unnecessary any', () => {
	it('FieldDifference uses union types instead of any', () => {
		const diff: FieldDifference = {
			field: 'title',
			nfo_value: 'original',
			scraped_value: 42,
			merged_value: true,
		};
		expect(diff.field).toBe('title');
		assertType<string | number | boolean | null | undefined>(diff.nfo_value);
		assertType<string | number | boolean | null | undefined>(diff.scraped_value);
		assertType<string | number | boolean | null | undefined>(diff.merged_value);
	});

	it('ActressMergeConflict uses union types instead of any', () => {
		const conflict: ActressMergeConflict = {
			field: 'first_name',
			target_value: 'Alice',
			source_value: null,
			default_resolution: 'target',
		};
		assertType<string | number | boolean | null | undefined>(conflict.target_value);
		assertType<string | number | boolean | null | undefined>(conflict.source_value);
	});

	it('ScraperOption.default uses union type instead of any', () => {
		const opt: ScraperOption = {
			key: 'timeout',
			label: 'Timeout',
			description: 'Request timeout',
			type: 'number',
			default: 30,
		};
		assertType<string | number | boolean | undefined>(opt.default);
	});

	it('ProxyTestRequest uses ProxyConfig and FlareSolverrConfig instead of any', () => {
		const req: ProxyTestRequest = {
			mode: 'direct',
			proxy: { enabled: true },
			flaresolverr: { enabled: true, url: 'http://localhost:8191', timeout: 30, max_retries: 3, session_ttl: 300 },
		};
		assertType<ProxyConfig>(req.proxy);
		assertType<FlareSolverrConfig | undefined>(req.flaresolverr);
	});

	it('ScrapersConfig has typed known fields', () => {
		const sc: ScrapersConfig = {
			user_agent: 'Mozilla/5.0',
			proxy: { enabled: false },
			flaresolverr: { enabled: false, url: '', timeout: 30, max_retries: 3, session_ttl: 300 },
		};
		assertType<string | undefined>(sc.user_agent);
		assertType<ProxyConfig | undefined>(sc.proxy);
		assertType<FlareSolverrConfig | undefined>(sc.flaresolverr);
	});

	it('ScrapersConfig allows scraper overrides via index', () => {
		const sc: ScrapersConfig = {};
		sc.javbus = { enabled: true, language: 'ja', timeout: 30, rate_limit: 0, retry_count: 0, user_agent: '', use_flaresolverr: false, use_browser: false };
		assertType<ScraperSettings | string | number | boolean | string[] | FlareSolverrConfig | ProxyConfig | undefined>(sc.javbus);
	});

	it('Config has all Go-backed sections typed', () => {
		const cfg: Config = {};
		assertType<ServerConfig | undefined>(cfg.server);
		assertType<APIConfig | undefined>(cfg.api);
		assertType<SystemConfig | undefined>(cfg.system);
		assertType<ScrapersConfig | undefined>(cfg.scrapers);
		assertType<MetadataConfig | undefined>(cfg.metadata);
		assertType<MatchingConfig | undefined>(cfg.file_matching);
		assertType<OutputConfig | undefined>(cfg.output);
		assertType<DatabaseConfig | undefined>(cfg.database);
		assertType<MediaInfoConfig | undefined>(cfg.mediainfo);
	});

	it('new config sub-interfaces exist and are properly typed', () => {
		const server: ServerConfig = { host: 'localhost', port: 8080 };
		const profile: ProxyProfile = { url: 'http://proxy:8080', username: 'user', password: 'pass' };
		const proxy: ProxyConfig = { enabled: true, profile: 'main', profiles: { main: profile } };
		const fs: FlareSolverrConfig = { enabled: true, url: 'http://localhost:8191', timeout: 30, max_retries: 3, session_ttl: 300 };
		const nfo: NFOConfig = { enabled: true, display_title: '{title}', filename_template: '{id}' };
		const fields: TranslationFieldsConfig = { title: true, description: true };
		const translation: TranslationConfig = { enabled: true, provider: 'openai' };
		const metadata: MetadataConfig = { translation };
		const db: DatabaseConfig = { type: 'sqlite', dsn: 'file:db.sqlite', log_level: 'warn' };
		const mi: MediaInfoConfig = { cli_enabled: false, cli_path: '', cli_timeout: 30 };

		expect(server.host).toBe('localhost');
		expect(proxy.enabled).toBe(true);
		expect(fs.url).toBe('http://localhost:8191');
		expect(nfo.enabled).toBe(true);
		expect(metadata.translation?.provider).toBe('openai');
		expect(db.type).toBe('sqlite');
	});
});
