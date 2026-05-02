import type {
	ScanRequest,
	ScanResponse,
	BrowseRequest,
	BrowseResponse,
	PathAutocompleteRequest,
	PathAutocompleteResponse,
	BatchScrapeRequest,
	BatchScrapeResponse,
	BatchJobResponse,
	HealthResponse,
	ErrorResponse,
	Movie,
	OrganizeRequest,
	OrganizeResponse,
	OrganizePreviewRequest,
	OrganizePreviewResponse,
	AvailableScrapersResponse,
	Scraper,
	RescrapeRequest,
	ScrapeRequest,
	BatchRescrapeRequest,
	BatchRescrapeResponse,
	PosterCropRequest,
	PosterCropResponse,
	PosterFromURLRequest,
	PosterFromURLResponse,
	Config,
	ProxyTestRequest,
	ProxyTestResponse,
	TranslationModelsRequest,
	TranslationModelsResponse,
	DeepLUsageRequest,
	DeepLUsageResponse,
	HistoryListResponse,
	HistoryListParams,
	HistoryStats,
	DeleteHistoryBulkParams,
	DeleteHistoryBulkResponse,
	JobListResponse,
	JobListItem,
	OperationListResponse,
	OperationItem,
	RevertResultResponse,
	RevertFileError,
	ActressListParams,
	ActressListResponse,
	ActressUpsertRequest,
	Actress,
	ActressMergePreviewRequest,
	ActressMergePreviewResponse,
	ActressMergeRequest,
	ActressMergeResponse,
	GenreReplacement,
	GenreReplacementListResponse,
	GenreReplacementCreateRequest,
	GenreReplacementUpdateRequest,
	WordReplacement,
	WordReplacementListResponse,
	WordReplacementCreateRequest,
	WordReplacementUpdateRequest,
	AuthCredentialsRequest,
	AuthStatusResponse,
	ImportResponse,
	GenreReplacementsImportRequest,
	WordReplacementsImportRequest,
	ActressesImportRequest,
	EventListResponse,
	EventListParams,
	EventStatsResponse,
	DeleteEventsParams,
	DeleteEventsResponse,
	UpdateRequest,
	VersionStatusResponse,
	BatchExcludeRequest,
	BatchExcludeResponse,
	BulkRescrapeRequest,
	BulkRescrapeResponse
} from './types';

// Build API base URL dynamically from browser location
// In production (Docker/deployed), frontend and backend are same-origin, so we use ''
// In dev mode with Vite proxy, we also use '' (proxy handles forwarding to backend)
// VITE_API_URL can override this for custom setups
function getAPIBaseURL(): string {
	// Allow explicit override via build-time env var (for special cases)
	if (import.meta.env.VITE_API_URL) {
		return import.meta.env.VITE_API_URL;
	}
	// Use empty string for same-origin requests (works with Vite proxy in dev and same-origin in production)
	return '';
}

class APIClient {
	private baseURL: string;

	constructor(baseURL?: string) {
		this.baseURL = baseURL ?? getAPIBaseURL();
	}

	public async request<T>(endpoint: string, options?: RequestInit): Promise<T> {
		const url = `${this.baseURL}${endpoint}`;
		const response = await fetch(url, {
			credentials: 'include',
			...options,
			headers: {
				'Content-Type': 'application/json',
				...options?.headers
			}
		});

		if (!response.ok) {
			const error: ErrorResponse = await response.json().catch(() => ({
				error: `HTTP ${response.status}: ${response.statusText}`
			}));
			throw new Error(error.error || 'API request failed');
		}

		const text = await response.text();
		if (!text || !text.trim()) return undefined as T;
		return JSON.parse(text) as T;
	}

	// Health check
	async health(): Promise<HealthResponse> {
		return this.request<HealthResponse>('/health');
	}

	// Get authentication state (first-run/setup/login gate)
	async getAuthStatus(): Promise<AuthStatusResponse> {
		return this.request<AuthStatusResponse>('/api/v1/auth/status');
	}

	// First-run setup: create initial single-user credentials and authenticate
	async setupAuth(credentials: AuthCredentialsRequest): Promise<AuthStatusResponse> {
		return this.request<AuthStatusResponse>('/api/v1/auth/setup', {
			method: 'POST',
			body: JSON.stringify(credentials)
		});
	}

	// Login with configured single-user credentials
	async loginAuth(credentials: AuthCredentialsRequest): Promise<AuthStatusResponse> {
		return this.request<AuthStatusResponse>('/api/v1/auth/login', {
			method: 'POST',
			body: JSON.stringify(credentials)
		});
	}

	// Logout current session
	async logoutAuth(): Promise<{ message: string }> {
		return this.request<{ message: string }>('/api/v1/auth/logout', {
			method: 'POST'
		});
	}

	// Build proxy URL for previewing remote images via backend (handles hotlink-protected hosts)
	getPreviewImageURL(imageURL: string): string {
		return `${this.baseURL}/api/v1/temp/image?url=${encodeURIComponent(imageURL)}`;
	}

	// Get current working directory
	async getCurrentWorkingDirectory(): Promise<{ path: string }> {
		return this.request<{ path: string }>('/api/v1/cwd');
	}

	// Scan directory for video files
	async scan(request: ScanRequest): Promise<ScanResponse> {
		return this.request<ScanResponse>('/api/v1/scan', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Browse filesystem
	async browse(request: BrowseRequest): Promise<BrowseResponse> {
		return this.request<BrowseResponse>('/api/v1/browse', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Autocomplete a partial filesystem path
	async autocompletePath(request: PathAutocompleteRequest): Promise<PathAutocompleteResponse> {
		return this.request<PathAutocompleteResponse>('/api/v1/browse/autocomplete', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Start batch scrape job
	async batchScrape(request: BatchScrapeRequest): Promise<BatchScrapeResponse> {
		return this.request<BatchScrapeResponse>('/api/v1/batch/scrape', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Get batch job status
	async getBatchJob(jobId: string, includeData = false): Promise<BatchJobResponse> {
		const params = includeData ? '?include_data=true' : '';
		return this.request<BatchJobResponse>(`/api/v1/batch/${jobId}${params}`);
	}

	// Cancel batch job
	async cancelBatchJob(jobId: string): Promise<void> {
		await this.request(`/api/v1/batch/${jobId}/cancel`, {
			method: 'POST'
		});
	}

	async deleteBatchJob(jobId: string): Promise<void> {
		await this.request(`/api/v1/batch/${jobId}`, {
			method: 'DELETE'
		});
	}

	async listBatchJobs(): Promise<{ jobs: BatchJobResponse[] }> {
		return this.request<{ jobs: BatchJobResponse[] }>('/api/v1/batch');
	}

	// Update movie in batch job
	async updateBatchMovie(jobId: string, movieId: string, movie: Movie): Promise<{ movie: Movie }> {
		return this.request<{ movie: Movie }>(`/api/v1/batch/${jobId}/movies/${movieId}`, {
			method: 'PATCH',
			body: JSON.stringify({ movie })
		});
	}

	// Update manual poster crop for a movie in batch review
	async updateBatchMoviePosterCrop(jobId: string, movieId: string, crop: PosterCropRequest): Promise<PosterCropResponse> {
		return this.request<PosterCropResponse>(`/api/v1/batch/${jobId}/movies/${movieId}/poster-crop`, {
			method: 'POST',
			body: JSON.stringify(crop)
		});
	}

	async updateBatchMoviePosterFromURL(jobId: string, movieId: string, request: PosterFromURLRequest): Promise<PosterFromURLResponse> {
		return this.request<PosterFromURLResponse>(`/api/v1/batch/${jobId}/movies/${movieId}/poster-from-url`, {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Exclude movie from batch organization
	async excludeBatchMovie(jobId: string, movieId: string): Promise<{ message: string }> {
		return this.request<{ message: string }>(`/api/v1/batch/${jobId}/movies/${movieId}/exclude`, {
			method: 'POST'
		});
	}

	async batchExcludeMovies(jobId: string, request: BatchExcludeRequest): Promise<BatchExcludeResponse> {
		return this.request<BatchExcludeResponse>(`/api/v1/batch/${jobId}/movies/batch-exclude`, {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	async bulkRescrapeMovies(jobId: string, request: BulkRescrapeRequest): Promise<BulkRescrapeResponse> {
		return this.request<BulkRescrapeResponse>(`/api/v1/batch/${jobId}/movies/batch-rescrape`, {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Organize scraped files
	async organizeBatchJob(jobId: string, request: OrganizeRequest): Promise<OrganizeResponse> {
		return this.request<OrganizeResponse>(`/api/v1/batch/${jobId}/organize`, {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Update batch job (generate NFOs and download media in place)
	async updateBatchJob(jobId: string, request?: UpdateRequest): Promise<{ message: string }> {
		const options: RequestInit = {
			method: 'POST'
		};
		if (request) {
			options.body = JSON.stringify(request);
		}
		return this.request<{ message: string }>(`/api/v1/batch/${jobId}/update`, options);
	}

	// Preview organize output
	async previewOrganize(jobId: string, movieId: string, request: OrganizePreviewRequest): Promise<OrganizePreviewResponse> {
		return this.request<OrganizePreviewResponse>(`/api/v1/batch/${jobId}/movies/${movieId}/preview`, {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Get movie by ID
	async getMovie(id: string): Promise<Movie> {
		const response = await this.request<{ movie: Movie }>(`/api/v1/movies/${id}`);
		return response.movie;
	}

	// List all movies
	async listMovies(limit?: number, offset?: number): Promise<{ movies: Movie[]; count: number }> {
		const params = new URLSearchParams();
		if (limit) params.set('limit', limit.toString());
		if (offset) params.set('offset', offset.toString());
		const query = params.toString() ? `?${params}` : '';
		return this.request(`/api/v1/movies${query}`);
	}

	// Get available scrapers
	async getAvailableScrapers(): Promise<AvailableScrapersResponse> {
		return this.request<AvailableScrapersResponse>('/api/v1/scrapers');
	}

	// Get scrapers (simplified version)
	async getScrapers(): Promise<Scraper[]> {
		const response = await this.getAvailableScrapers();
		return response.scrapers.map(s => ({
			name: s.name,
			display_title: s.display_title,
			enabled: s.enabled,
			options: s.options || {}
		}));
	}

	// Rescrape movie with selected scrapers
	async rescrapeMovie(id: string, req: RescrapeRequest): Promise<Movie> {
		const response = await this.request<{ movie: Movie }>(`/api/v1/movies/${id}/rescrape`, {
			method: 'POST',
			body: JSON.stringify(req)
		});
		return response.movie;
	}

	// Scrape movie from content-id/dvd-id or URL
	async scrapeMovie(input: string, options?: { force?: boolean; selected_scrapers?: string[] }): Promise<Movie> {
		const request: ScrapeRequest = {
			id: input,
			force: options?.force,
			selected_scrapers: options?.selected_scrapers
		};
		const response = await this.request<{ movie: Movie }>('/api/v1/scrape', {
			method: 'POST',
			body: JSON.stringify(request)
		});
		return response.movie;
	}

	// Rescrape movie within a batch job (batch-aware rescrape)
	async rescrapeBatchMovie(jobId: string, movieId: string, req: BatchRescrapeRequest): Promise<BatchRescrapeResponse> {
		return this.request<BatchRescrapeResponse>(`/api/v1/batch/${jobId}/movies/${movieId}/rescrape`, {
			method: 'POST',
			body: JSON.stringify(req)
		});
	}

	// Get server configuration
	async getConfig(): Promise<Config> {
		return this.request<Config>('/api/v1/config');
	}

	// Test proxy or FlareSolverr connectivity
	async testProxy(request: ProxyTestRequest): Promise<ProxyTestResponse> {
		return this.request<ProxyTestResponse>('/api/v1/proxy/test', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Discover models from OpenAI-compatible provider
	async getTranslationModels(request: TranslationModelsRequest): Promise<TranslationModelsResponse> {
		return this.request<TranslationModelsResponse>('/api/v1/translation/models', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	async getDeepLUsage(request: DeepLUsageRequest): Promise<DeepLUsageResponse> {
		return this.request<DeepLUsageResponse>('/api/v1/translation/deepl/usage', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// List actresses with pagination and optional search query
	async listActresses(params?: ActressListParams): Promise<ActressListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		if (params?.q) queryParams.set('q', params.q);
		if (params?.sort_by) queryParams.set('sort_by', params.sort_by);
		if (params?.sort_order) queryParams.set('sort_order', params.sort_order);
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<ActressListResponse>(`/api/v1/actresses${query}`);
	}

	// Get actress by ID
	async getActress(id: number): Promise<Actress> {
		return this.request<Actress>(`/api/v1/actresses/${id}`);
	}

	// Create actress
	async createActress(request: ActressUpsertRequest): Promise<Actress> {
		return this.request<Actress>('/api/v1/actresses', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Update actress
	async updateActress(id: number, request: ActressUpsertRequest): Promise<Actress> {
		return this.request<Actress>(`/api/v1/actresses/${id}`, {
			method: 'PUT',
			body: JSON.stringify(request)
		});
	}

	// Delete actress
	async deleteActress(id: number): Promise<void> {
		await this.request(`/api/v1/actresses/${id}`, { method: 'DELETE' });
	}

	// Preview merge result between two actresses
	async previewActressMerge(request: ActressMergePreviewRequest): Promise<ActressMergePreviewResponse> {
		return this.request<ActressMergePreviewResponse>('/api/v1/actresses/merge/preview', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Apply merge between two actresses
	async mergeActresses(request: ActressMergeRequest): Promise<ActressMergeResponse> {
		return this.request<ActressMergeResponse>('/api/v1/actresses/merge', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// List genre replacements with pagination
	async listGenreReplacements(params?: { limit?: number; offset?: number }): Promise<GenreReplacementListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<GenreReplacementListResponse>(`/api/v1/genres/replacements${query}`);
	}

	// Create a genre replacement (idempotent — returns existing if original already exists)
	async createGenreReplacement(request: GenreReplacementCreateRequest): Promise<GenreReplacement> {
		return this.request<GenreReplacement>('/api/v1/genres/replacements', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Delete a genre replacement by id
	async deleteGenreReplacement(id: number): Promise<void> {
		await this.request(`/api/v1/genres/replacements?id=${id}`, {
			method: 'DELETE'
		});
	}
	// Update a genre replacement
	async updateGenreReplacement(request: GenreReplacementUpdateRequest): Promise<GenreReplacement> {
		return this.request<GenreReplacement>('/api/v1/genres/replacements', {
			method: 'PUT',
			body: JSON.stringify(request)
		});
	}

	// List word replacements with pagination
	async listWordReplacements(params?: { limit?: number; offset?: number }): Promise<WordReplacementListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<WordReplacementListResponse>(`/api/v1/words/replacements${query}`);
	}

	// Create a word replacement (idempotent)
	async createWordReplacement(request: WordReplacementCreateRequest): Promise<WordReplacement> {
		return this.request<WordReplacement>('/api/v1/words/replacements', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Update a word replacement
	async updateWordReplacement(request: WordReplacementUpdateRequest): Promise<WordReplacement> {
		return this.request<WordReplacement>('/api/v1/words/replacements', {
			method: 'PUT',
			body: JSON.stringify(request)
		});
	}

	// Delete a word replacement by id
	async deleteWordReplacement(id: number): Promise<void> {
		await this.request(`/api/v1/words/replacements?id=${id}`, {
			method: 'DELETE'
		});
	}

	// Export genre replacements as JSON array
	async exportGenreReplacements(): Promise<GenreReplacement[]> {
		return this.request<GenreReplacement[]>('/api/v1/genres/replacements/export', {
			method: 'GET'
		});
	}

	// Import genre replacements from JSON
	async importGenreReplacements(request: GenreReplacementsImportRequest): Promise<ImportResponse> {
		return this.request<ImportResponse>('/api/v1/genres/replacements/import', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Export word replacements as JSON array
	async exportWordReplacements(): Promise<WordReplacement[]> {
		return this.request<WordReplacement[]>('/api/v1/words/replacements/export', {
			method: 'GET'
		});
	}

	// Import word replacements from JSON
	async importWordReplacements(request: WordReplacementsImportRequest): Promise<ImportResponse> {
		return this.request<ImportResponse>('/api/v1/words/replacements/import', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Export actresses as JSON array
	async exportActresses(): Promise<Actress[]> {
		return this.request<Actress[]>('/api/v1/actresses/export', {
			method: 'GET'
		});
	}

	// Import actresses from JSON
	async importActresses(request: ActressesImportRequest): Promise<ImportResponse> {
		return this.request<ImportResponse>('/api/v1/actresses/import', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Get history records with optional filtering
	async getHistory(params?: HistoryListParams): Promise<HistoryListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		if (params?.operation) queryParams.set('operation', params.operation);
		if (params?.status) queryParams.set('status', params.status);
		if (params?.movie_id) queryParams.set('movie_id', params.movie_id);
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<HistoryListResponse>(`/api/v1/history${query}`);
	}

	// Get history statistics
	async getHistoryStats(): Promise<HistoryStats> {
		return this.request<HistoryStats>('/api/v1/history/stats');
	}

	// Delete a single history record
	async deleteHistory(id: number): Promise<void> {
		await this.request(`/api/v1/history/${id}`, { method: 'DELETE' });
	}

	// Delete history records in bulk
	async deleteHistoryBulk(params: DeleteHistoryBulkParams): Promise<DeleteHistoryBulkResponse> {
		const queryParams = new URLSearchParams();
		if (params.older_than_days) queryParams.set('older_than_days', params.older_than_days.toString());
		if (params.movie_id) queryParams.set('movie_id', params.movie_id);
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<DeleteHistoryBulkResponse>(`/api/v1/history${query}`, { method: 'DELETE' });
	}

	// List batch jobs with optional status filter
	async listOrganizedJobs(params?: { status?: string; limit?: number; offset?: number }): Promise<JobListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.status) queryParams.set('status', params.status);
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<JobListResponse>(`/api/v1/jobs${query}`);
	}

	// Get a single batch job by ID
	async getJob(jobId: string): Promise<JobListItem> {
		return this.request<JobListItem>(`/api/v1/jobs/${jobId}`);
	}

	// Get operations for a specific batch job
	async getJobOperations(jobId: string): Promise<OperationListResponse> {
		return this.request<OperationListResponse>(`/api/v1/jobs/${jobId}/operations`);
	}

	// Revert an entire batch job
	async revertBatchJob(jobId: string): Promise<RevertResultResponse> {
		return this.request<RevertResultResponse>(`/api/v1/jobs/${jobId}/revert`, {
			method: 'POST'
		});
	}

	// Revert a single operation within a batch job
	async revertJobOperation(jobId: string, movieId: string): Promise<RevertResultResponse> {
		return this.request<RevertResultResponse>(`/api/v1/jobs/${jobId}/operations/${movieId}/revert`, {
			method: 'POST'
		});
	}

	// List events with optional filtering
	async listEvents(params?: EventListParams): Promise<EventListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.type) queryParams.set('type', params.type);
		if (params?.severity) queryParams.set('severity', params.severity);
		if (params?.source) queryParams.set('source', params.source);
		if (params?.start) queryParams.set('start', params.start);
		if (params?.end) queryParams.set('end', params.end);
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<EventListResponse>(`/api/v1/events${query}`);
	}

	// Get event statistics
	async getEventStats(): Promise<EventStatsResponse> {
		return this.request<EventStatsResponse>('/api/v1/events/stats');
	}

	// Delete events older than N days
	async deleteEvents(params: DeleteEventsParams): Promise<DeleteEventsResponse> {
		const queryParams = new URLSearchParams();
		queryParams.set('older_than_days', params.older_than_days.toString());
		return this.request<DeleteEventsResponse>(`/api/v1/events?${queryParams}`, { method: 'DELETE' });
	}

	async getVersionStatus(): Promise<VersionStatusResponse> {
		return this.request<VersionStatusResponse>('/api/v1/version');
	}

	async checkVersion(): Promise<VersionStatusResponse> {
		return this.request<VersionStatusResponse>('/api/v1/version/check', { method: 'POST' });
	}
}

export const apiClient = new APIClient();
export default apiClient;
