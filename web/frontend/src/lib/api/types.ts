// API Request/Response Types

export interface FileInfo {
	name: string;
	path: string;
	is_dir: boolean;
	size: number;
	mod_time: string;
	movie_id?: string;
	matched?: boolean;
}

export interface ScanRequest {
	path: string;
	recursive: boolean;
	filter?: string; // Filter folder/file names (case-insensitive substring match)
}

export interface ScanResponse {
	files: FileInfo[];
	count: number;
	skipped?: string[];
}

export interface BrowseRequest {
	path: string;
}

export interface BrowseResponse {
	current_path: string;
	parent_path?: string;
	items: FileInfo[];
}

export interface PathAutocompleteRequest {
	path: string;
	limit?: number;
}

export interface PathAutocompleteSuggestion {
	name: string;
	path: string;
	is_dir: boolean;
}

export interface PathAutocompleteResponse {
	input_path: string;
	base_path: string;
	suggestions: PathAutocompleteSuggestion[];
}

export interface ScrapeRequest {
	id: string;
	force?: boolean;
	selected_scrapers?: string[];
}

export type OperationMode = 'organize' | 'in-place' | 'in-place-norenamefolder' | 'metadata-only' | 'preview';

export interface BatchScrapeRequest {
	files: string[];
	strict: boolean;
	force: boolean;
	destination?: string;
	update?: boolean;
	selected_scrapers?: string[];
	preset?: 'conservative' | 'gap-fill' | 'aggressive'; // Merge strategy preset (overrides scalar/array)
	scalar_strategy?: 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only';
	array_strategy?: 'merge' | 'replace';
	move_to_folder?: boolean; // Override config.output.move_to_folder
	rename_folder_in_place?: boolean; // Override config.output.rename_folder_in_place
	operation_mode?: OperationMode; // Per-request override of config operation_mode. Takes priority over boolean fields when set.
}

export interface RescrapeRequest {
	selected_scrapers: string[];
	force?: boolean;
}

export interface PosterCropRequest {
	x: number;
	y: number;
	width: number;
	height: number;
}

export interface PosterCropResponse {
	cropped_poster_url: string;
}

export interface BatchRescrapeRequest {
	force?: boolean;
	selected_scrapers?: string[];
	manual_search_input?: string;
	preset?: 'conservative' | 'gap-fill' | 'aggressive'; // Merge strategy preset (overrides scalar/array)
	scalar_strategy?: 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only';
	array_strategy?: 'merge' | 'replace';
}

export interface BatchRescrapeResponse {
	movie: Movie;
	field_sources?: Record<string, string>;
	actress_sources?: Record<string, string>;
}

export interface DataSource {
	source: string; // "scraper" or "nfo"
	confidence: number; // 0.0-1.0
	last_updated?: string; // ISO 8601 timestamp
}

export interface MergeStatistics {
	total_fields: number;
	from_scraper: number;
	from_nfo: number;
	merged_arrays: number;
	conflicts_resolved: number;
	empty_fields: number;
}

export interface FieldDifference {
	field: string;
	nfo_value?: any;
	scraped_value?: any;
	merged_value?: any;
	reason?: string;
}

export interface NFOComparisonRequest {
	nfo_path?: string;
	merge_strategy?: 'prefer-scraper' | 'prefer-nfo' | 'merge-arrays'; // Deprecated: use preset or scalar/array strategies
	preset?: 'conservative' | 'gap-fill' | 'aggressive';
	scalar_strategy?: 'prefer-nfo' | 'prefer-scraper' | 'preserve-existing' | 'fill-missing-only';
	array_strategy?: 'merge' | 'replace';
	selected_scrapers?: string[];
}

export interface NFOComparisonResponse {
	movie_id: string;
	nfo_exists: boolean;
	nfo_path?: string;
	nfo_data?: Movie;
	scraped_data?: Movie;
	merged_data?: Movie;
	provenance?: Record<string, DataSource>;
	merge_stats?: MergeStatistics;
	differences?: FieldDifference[];
}

export interface BatchScrapeResponse {
	job_id: string;
}

export interface FileResult {
	file_path: string;
	movie_id: string;
	status: string;
	error?: string;
	poster_error?: string;
	translation_warning?: string;
	field_sources?: Record<string, string>;
	actress_sources?: Record<string, string>;
	data?: Movie;
	started_at: string;
	ended_at?: string;
	is_multi_part?: boolean;
	part_number?: number;
	part_suffix?: string;
}

export interface BatchJobResponse {
	id: string;
	status: string;
	total_files: number;
	completed: number;
	failed: number;
	operation_count: number;
	reverted_count: number;
	excluded: Record<string, boolean>;
	progress: number;
	destination: string;
	results: Record<string, FileResult>;
	files?: string[];
	started_at: string;
	completed_at?: string;
	operation_mode_override?: string;
}

export interface ProgressMessage {
	job_id: string;
	file_index: number;
	file_path: string;
	status: string;
	progress: number;
	message: string;
	error?: string;
}

export interface Genre {
	id?: number;
	name: string;
}

export interface GenreReplacement {
	id: number;
	original: string;
	replacement: string;
	created_at: string;
	updated_at: string;
}

export interface GenreReplacementListResponse {
	replacements: GenreReplacement[];
	count: number;
	total: number;
	limit: number;
	offset: number;
}

export interface GenreReplacementCreateRequest {
	original: string;
	replacement: string;
}

export interface Movie {
	id: string;
	content_id?: string;
	title: string;
	display_title?: string;
	original_title?: string;
	description?: string;
	release_date?: string;
	runtime?: number;
	director?: string;
	maker?: string;
	label?: string;
	series?: string;
	rating_score?: number;
	rating_votes?: number;
	genres?: Genre[];
	actresses?: Actress[];
	cover_url?: string;
	poster_url?: string;
	cropped_poster_url?: string;
	should_crop_poster?: boolean;
	screenshot_urls?: string[];
	trailer_url?: string;
	original_file_name?: string;
	created_at?: string;
	updated_at?: string;
}

export interface Actress {
	id?: number;
	dmm_id?: number;
	first_name?: string;
	last_name?: string;
	japanese_name?: string;
	thumb_url?: string;
	aliases?: string;
}

export interface ActressListParams {
	limit?: number;
	offset?: number;
	q?: string;
	sort_by?: 'name' | 'japanese_name' | 'id' | 'dmm_id' | 'updated_at' | 'created_at';
	sort_order?: 'asc' | 'desc';
}

export interface ActressListResponse {
	actresses: Actress[];
	count: number;
	total: number;
	limit: number;
	offset: number;
}

export interface ActressUpsertRequest {
	dmm_id?: number;
	first_name?: string;
	last_name?: string;
	japanese_name?: string;
	thumb_url?: string;
	aliases?: string;
}

export type ActressMergeResolution = 'target' | 'source';

export interface ActressMergePreviewRequest {
	target_id: number;
	source_id: number;
}

export interface ActressMergeConflict {
	field: 'dmm_id' | 'first_name' | 'last_name' | 'japanese_name' | 'thumb_url';
	target_value?: any;
	source_value?: any;
	default_resolution: ActressMergeResolution;
}

export interface ActressMergePreviewResponse {
	target: Actress;
	source: Actress;
	proposed_merged: Actress;
	conflicts: ActressMergeConflict[];
	default_resolutions: Record<string, ActressMergeResolution>;
}

export interface ActressMergeRequest {
	target_id: number;
	source_id: number;
	resolutions?: Record<string, ActressMergeResolution>;
}

export interface ActressMergeResponse {
	merged_actress: Actress;
	merged_from_id: number;
	updated_movies: number;
	conflicts_resolved: number;
	aliases_added: number;
}

export interface ErrorResponse {
	error: string;
	errors?: string[];
}

export interface AuthStatusResponse {
	initialized: boolean;
	authenticated: boolean;
	username?: string;
}

export interface AuthCredentialsRequest {
	username: string;
	password: string;
	remember_me?: boolean;
}

export interface HealthResponse {
	status: string;
	scrapers: string[];
	version?: string;
	commit?: string;
	build_date?: string;
}

export interface UpdateRequest {
	force_overwrite?: boolean;
	preserve_nfo?: boolean;
	preset?: 'conservative' | 'gap-fill' | 'aggressive';
	scalar_strategy?: 'prefer-scraper' | 'prefer-nfo' | 'preserve-existing' | 'fill-missing-only';
	array_strategy?: 'merge' | 'replace';
	skip_nfo?: boolean;
	skip_download?: boolean;
}

export interface OrganizeRequest {
	destination: string;
	copy_only?: boolean;
	link_mode?: 'hard' | 'soft';
	operation_mode?: OperationMode;
	skip_nfo?: boolean;
	skip_download?: boolean;
}

export interface OrganizeResponse {
	message: string;
}

export interface OrganizePreviewRequest {
	destination: string;
	copy_only?: boolean;
	link_mode?: 'hard' | 'soft';
	operation_mode?: OperationMode;
	skip_nfo?: boolean;
	skip_download?: boolean;
}

export interface OrganizePreviewResponse {
	folder_name: string;
	file_name: string;
	subfolder_path?: string; // Subfolder hierarchy relative to destination (e.g. "Studio/2025")
	full_path: string;
	video_files?: string[]; // For multi-part files: all video file paths
	nfo_path?: string; // Single NFO (backward compatibility) - empty if NFO disabled
	nfo_paths?: string[]; // For per_file=true multi-part: all NFO file paths
	poster_path?: string; // Empty if cover/poster download disabled
	fanart_path?: string; // Empty if fanart download disabled
	extrafanart_path?: string; // Empty if extrafanart download disabled
	screenshots?: string[]; // Empty if extrafanart download disabled
	source_path?: string; // Original file path (for in-place modes)
	operation_mode?: string;
}

export interface ScraperOption {
	key: string;
	label: string;
	description: string;
	type: string; // 'boolean', 'string', 'number', etc.
	default?: any; // Default value for this option
	min?: number; // For number type
	max?: number; // For number type
	unit?: string; // For number type (e.g., 'seconds', 'MB')
	choices?: { value: string; label: string }[]; // For select type
}

export interface ScraperInfo {
	name: string;
	display_title: string;
	enabled: boolean;
	options?: ScraperOption[];
}

export interface Scraper {
	name: string;
	display_title: string;
	enabled: boolean;
	options?: Record<string, any>;
}

export interface AvailableScrapersResponse {
	scrapers: ScraperInfo[];
}

export interface ProxyTestRequest {
	mode: 'direct' | 'flaresolverr';
	proxy: any;
	flaresolverr?: any;
	target_url?: string;
}

export interface ProxyTestResponse {
	success: boolean;
	mode: 'direct' | 'flaresolverr';
	target_url: string;
	status_code?: number;
	duration_ms: number;
	message: string;
	proxy_url?: string;
	flaresolverr_url?: string;
	verification_token?: string;
	token_expires_at?: number;
}

export interface TranslationModelsRequest {
	provider: 'openai' | 'openai-compatible' | 'anthropic';
	base_url: string;
	api_key: string;
}

export interface TranslationModelsResponse {
	models: string[];
}

export interface OpenAICompatibleTranslationConfig {
	base_url: string;
	api_key: string;
	model: string;
	enable_thinking?: boolean;
}

export interface AnthropicTranslationConfig {
	base_url: string;
	api_key: string;
	model: string;
}

export interface DeepLUsageRequest {
	mode: string;
	base_url: string;
	api_key: string;
}

export interface DeepLUsageResponse {
	character_count: number;
	character_limit: number;
	start_time?: string;
	end_time?: string;
	api_key_character_count?: number;
	api_key_character_limit?: number;
}

export interface TestResult {
	success: boolean;
	timestamp: number;
	message?: string;
	configSnapshot?: string;
	verificationToken?: string;
	tokenExpiresAt?: number;
}

// Config types
export interface PerformanceConfig {
	max_workers: number;
	worker_timeout: number;
	buffer_size: number;
	update_interval: number;
}

export interface Config {
	performance: PerformanceConfig;
	// Other config fields can be added here as needed
	[key: string]: any;
}

// History types
export interface HistoryRecord {
	id: number;
	movie_id: string;
	operation: 'scrape' | 'organize' | 'download' | 'nfo';
	original_path: string;
	new_path: string;
	status: 'success' | 'failed' | 'reverted';
	error_message: string;
	metadata: string;
	dry_run: boolean;
	created_at: string;
}

export interface HistoryStats {
	total: number;
	success: number;
	failed: number;
	reverted: number;
	by_operation: {
		scrape: number;
		organize: number;
		download: number;
		nfo: number;
	};
}

export interface HistoryListResponse {
	records: HistoryRecord[];
	total: number;
	limit: number;
	offset: number;
}

export interface HistoryListParams {
	limit?: number;
	offset?: number;
	operation?: string;
	status?: string;
	movie_id?: string;
}

export interface DeleteHistoryBulkParams {
	older_than_days?: number;
	movie_id?: string;
}

export interface DeleteHistoryBulkResponse {
	deleted: number;
}

// Batch job types (History & Revert — Phase 5)
export interface JobListItem {
	id: string;
	status: string;
	total_files: number;
	completed: number;
	failed: number;
	operation_count: number;
	reverted_count: number;
	progress: number;
	destination: string;
	started_at: string;
	completed_at?: string;
	organized_at?: string;
	reverted_at?: string;
}

export interface JobListResponse {
	jobs: JobListItem[];
}

export interface OperationItem {
	id: number;
	movie_id: string;
	original_path: string;
	new_path: string;
	operation_type: string;
	revert_status: string;
	reverted_at?: string;
	in_place_renamed: boolean;
	created_at: string;
}

export interface OperationListResponse {
	job_id: string;
	job_status: string;
	operations: OperationItem[];
	total: number;
}

export interface RevertResultResponse {
	job_id: string;
	status: string;
	total: number;
	succeeded: number;
	failed: number;
	errors?: RevertFileError[];
}

export interface RevertFileError {
	operation_id: number;
	movie_id: string;
	original_path: string;
	new_path: string;
	error: string;
}

// Event types (Logs page)
export interface EventItem {
	id: number;
	event_type: string;
	severity: string;
	message: string;
	context: string;
	source: string;
	created_at: string;
}

export interface EventListResponse {
	events: EventItem[];
	total: number;
}

export interface EventStatsResponse {
	total: number;
	by_type: Record<string, number>;
	by_severity: Record<string, number>;
	by_source: Record<string, number>;
}

export interface EventListParams {
	type?: string;
	severity?: string;
	source?: string;
	start?: string;
	end?: string;
	limit?: number;
	offset?: number;
}

export interface DeleteEventsParams {
	older_than_days: number;
}

export interface DeleteEventsResponse {
	deleted: number;
	message: string;
}

export interface VersionStatusResponse {
	current: string;
	latest: string;
	update_available: boolean;
	prerelease: boolean;
	checked_at: string;
	source: string;
	error?: string;
}
