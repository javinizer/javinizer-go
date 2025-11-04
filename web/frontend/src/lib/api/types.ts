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
}

export interface RescrapeRequest {
	selected_scrapers: string[];
	force?: boolean;
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
	temp_poster_url?: string;
	started_at: string;
	ended_at?: string;
}

export interface BatchJobResponse {
	id: string;
	status: string;
	total_files: number;
	completed: number;
	failed: number;
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
	should_crop_poster?: boolean;
	screenshot_urls?: string[];
	trailer_url?: string;
	original_file_name?: string;
	created_at?: string;
	updated_at?: string;
}

export interface Actress {
	id?: number;
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
	nfo_path: string;
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
