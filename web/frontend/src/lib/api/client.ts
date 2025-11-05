import type {
	ScanRequest,
	ScanResponse,
	BrowseRequest,
	BrowseResponse,
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
	BatchRescrapeResponse
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

		return response.json();
	}

	// Health check
	async health(): Promise<HealthResponse> {
		return this.request<HealthResponse>('/health');
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

	// Start batch scrape job
	async batchScrape(request: BatchScrapeRequest): Promise<BatchScrapeResponse> {
		return this.request<BatchScrapeResponse>('/api/v1/batch/scrape', {
			method: 'POST',
			body: JSON.stringify(request)
		});
	}

	// Get batch job status
	async getBatchJob(jobId: string): Promise<BatchJobResponse> {
		return this.request<BatchJobResponse>(`/api/v1/batch/${jobId}`);
	}

	// Cancel batch job
	async cancelBatchJob(jobId: string): Promise<void> {
		await this.request(`/api/v1/batch/${jobId}/cancel`, {
			method: 'POST'
		});
	}

	// Update movie in batch job
	async updateBatchMovie(jobId: string, movieId: string, movie: Movie): Promise<{ movie: Movie }> {
		return this.request<{ movie: Movie }>(`/api/v1/batch/${jobId}/movies/${movieId}`, {
			method: 'PATCH',
			body: JSON.stringify({ movie })
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
	async updateBatchJob(jobId: string): Promise<{ message: string }> {
		return this.request<{ message: string }>(`/api/v1/batch/${jobId}/update`, {
			method: 'POST'
		});
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
		return this.request<Movie>(`/api/v1/movie/${id}`);
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
			display_name: s.display_name,
			enabled: s.enabled,
			options: s.options || {}
		}));
	}

	// Rescrape movie with selected scrapers
	async rescrapeMovie(id: string, req: RescrapeRequest): Promise<Movie> {
		const response = await this.request<{ movie: Movie }>(`/api/v1/movie/${id}/rescrape`, {
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
}

export const apiClient = new APIClient();
export default apiClient;
