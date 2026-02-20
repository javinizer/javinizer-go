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

export interface ScrapeRequest {
	id: string;
	force?: boolean;
	selected_scrapers?: string[];
}

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
	excluded: Record<string, boolean>; // Map of file paths excluded from organization
	progress: number;
	results: Record<string, FileResult>;
	files?: string[]; // List of all file paths in the job
	started_at: string;
	completed_at?: string;
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

export interface Movie {
	id: string;
	content_id?: string;
	title: string;
	display_name?: string;
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

export interface ErrorResponse {
	error: string;
	errors?: string[];
}

export interface HealthResponse {
	status: string;
	scrapers: string[];
}

export interface OrganizeRequest {
	destination: string;
	copy_only?: boolean;
}

export interface OrganizeResponse {
	message: string;
}

export interface OrganizePreviewRequest {
	destination: string;
	copy_only?: boolean;
}

export interface OrganizePreviewResponse {
	folder_name: string;
	file_name: string;
	full_path: string;
	video_files?: string[]; // For multi-part files: all video file paths
	nfo_path: string; // Single NFO (backward compatibility)
	nfo_paths?: string[]; // For per_file=true multi-part: all NFO file paths
	poster_path: string;
	fanart_path: string;
	extrafanart_path: string;
	screenshots: string[];
}

export interface ScraperOption {
	key: string;
	label: string;
	description: string;
	type: string; // 'boolean', 'string', 'number', etc.
	min?: number; // For number type
	max?: number; // For number type
	unit?: string; // For number type (e.g., 'seconds', 'MB')
	choices?: { value: string; label: string }[]; // For select type
}

export interface ScraperInfo {
	name: string;
	display_name: string;
	enabled: boolean;
	options?: ScraperOption[];
}

export interface Scraper {
	name: string;
	display_name: string;
	enabled: boolean;
	options?: Record<string, any>;
}

export interface AvailableScrapersResponse {
	scrapers: ScraperInfo[];
}

export interface ProxyTestRequest {
	mode: 'direct' | 'flaresolverr';
	proxy: any;
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
}

export interface TranslationModelsRequest {
	provider: 'openai';
	base_url: string;
	api_key: string;
}

export interface TranslationModelsResponse {
	models: string[];
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
