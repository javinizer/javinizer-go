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
	UpdateRequest,
	BatchExcludeRequest,
	BatchExcludeResponse,
	BulkRescrapeRequest,
	BulkRescrapeResponse,
	SourceResultsResponse,
	FieldOverrideRequest,
	FieldOverrideResponse,
} from '../types';
import { BaseClient } from './common';

// JobClient handles batch job lifecycle: scrape, organize, update, rescrape, revert.
export class JobClient extends BaseClient {
	async batchScrape(request: BatchScrapeRequest): Promise<BatchScrapeResponse> {
		return this.request<BatchScrapeResponse>('/api/v1/batch/scrape', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async getBatchJob(jobId: string, includeData = false): Promise<BatchJobResponse> {
		const params = includeData ? '?include_data=true' : '';
		return this.request<BatchJobResponse>(`/api/v1/batch/${jobId}${params}`);
	}

	async cancelBatchJob(jobId: string): Promise<void> {
		await this.request(`/api/v1/batch/${jobId}/cancel`, { method: 'POST' });
	}

	async deleteBatchJob(jobId: string): Promise<void> {
		await this.request(`/api/v1/batch/${jobId}`, { method: 'DELETE' });
	}

	async listBatchJobs(): Promise<{ jobs: BatchJobResponse[] }> {
		return this.request<{ jobs: BatchJobResponse[] }>('/api/v1/batch');
	}

	async updateBatchMovie(jobId: string, resultId: string, movie: Movie): Promise<{ movie: Movie }> {
		return this.request<{ movie: Movie }>(`/api/v1/batch/${jobId}/results/${resultId}`, {
			method: 'PATCH',
			body: JSON.stringify({ movie }),
		});
	}

	async updateBatchMoviePosterCrop(
		jobId: string,
		resultId: string,
		crop: PosterCropRequest,
	): Promise<PosterCropResponse> {
		return this.request<PosterCropResponse>(
			`/api/v1/batch/${jobId}/results/${resultId}/poster-crop`,
			{
				method: 'POST',
				body: JSON.stringify(crop),
			},
		);
	}

	async updateBatchMoviePosterFromURL(
		jobId: string,
		resultId: string,
		request: PosterFromURLRequest,
	): Promise<PosterFromURLResponse> {
		return this.request<PosterFromURLResponse>(
			`/api/v1/batch/${jobId}/results/${resultId}/poster-from-url`,
			{
				method: 'POST',
				body: JSON.stringify(request),
			},
		);
	}

	async getBatchMovieSources(jobId: string, resultId: string): Promise<SourceResultsResponse> {
		return this.request<SourceResultsResponse>(
			`/api/v1/batch/${jobId}/results/${resultId}/sources`,
		);
	}

	async overrideBatchMovieField(
		jobId: string,
		resultId: string,
		request: FieldOverrideRequest,
	): Promise<FieldOverrideResponse> {
		return this.request<FieldOverrideResponse>(
			`/api/v1/batch/${jobId}/results/${resultId}/field-override`,
			{
				method: 'POST',
				body: JSON.stringify(request),
			},
		);
	}

	async excludeBatchMovie(jobId: string, resultId: string): Promise<{ message: string }> {
		return this.request<{ message: string }>(`/api/v1/batch/${jobId}/results/${resultId}/exclude`, {
			method: 'POST',
		});
	}

	async batchExcludeMovies(
		jobId: string,
		request: BatchExcludeRequest,
	): Promise<BatchExcludeResponse> {
		return this.request<BatchExcludeResponse>(`/api/v1/batch/${jobId}/movies/batch-exclude`, {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async bulkRescrapeMovies(
		jobId: string,
		request: BulkRescrapeRequest,
	): Promise<BulkRescrapeResponse> {
		return this.request<BulkRescrapeResponse>(`/api/v1/batch/${jobId}/movies/batch-rescrape`, {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async organizeBatchJob(jobId: string, request: OrganizeRequest): Promise<OrganizeResponse> {
		return this.request<OrganizeResponse>(`/api/v1/batch/${jobId}/organize`, {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async updateBatchJob(jobId: string, request?: UpdateRequest): Promise<{ message: string }> {
		const options: RequestInit = { method: 'POST' };
		if (request) {
			options.body = JSON.stringify(request);
		}
		return this.request<{ message: string }>(`/api/v1/batch/${jobId}/update`, options);
	}

	async previewOrganize(
		jobId: string,
		resultId: string,
		request: OrganizePreviewRequest,
	): Promise<OrganizePreviewResponse> {
		return this.request<OrganizePreviewResponse>(
			`/api/v1/batch/${jobId}/results/${resultId}/preview`,
			{
				method: 'POST',
				body: JSON.stringify(request),
			},
		);
	}

	async rescrapeBatchMovie(
		jobId: string,
		resultId: string,
		req: BatchRescrapeRequest,
	): Promise<BatchRescrapeResponse> {
		return this.request<BatchRescrapeResponse>(
			`/api/v1/batch/${jobId}/results/${resultId}/rescrape`,
			{
				method: 'POST',
				body: JSON.stringify(req),
			},
		);
	}
}

// FileClient handles filesystem scanning, browsing, and path autocompletion.
export class FileClient extends BaseClient {
	async scan(request: ScanRequest): Promise<ScanResponse> {
		return this.request<ScanResponse>('/api/v1/scan', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async browse(request: BrowseRequest): Promise<BrowseResponse> {
		return this.request<BrowseResponse>('/api/v1/browse', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async autocompletePath(request: PathAutocompleteRequest): Promise<PathAutocompleteResponse> {
		return this.request<PathAutocompleteResponse>('/api/v1/browse/autocomplete', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}
}

// ScraperClient handles standalone scrape and scraper metadata endpoints.
export class ScraperClient extends BaseClient {
	async getAvailableScrapers(): Promise<AvailableScrapersResponse> {
		return this.request<AvailableScrapersResponse>('/api/v1/scrapers');
	}

	async getScrapers(): Promise<Scraper[]> {
		const response = await this.getAvailableScrapers();
		return response.scrapers.map((s) => ({
			name: s.name,
			display_title: s.display_title,
			enabled: s.enabled,
			options: s.options || {},
		}));
	}

	async rescrapeMovie(id: string, req: RescrapeRequest): Promise<Movie> {
		const response = await this.request<{ movie: Movie }>(`/api/v1/movies/${id}/rescrape`, {
			method: 'POST',
			body: JSON.stringify(req),
		});
		return response.movie;
	}

	async scrapeMovie(
		input: string,
		options?: { force?: boolean; selected_scrapers?: string[] },
	): Promise<Movie> {
		const request: ScrapeRequest = {
			id: input,
			force: options?.force,
			selected_scrapers: options?.selected_scrapers,
		};
		const response = await this.request<{ movie: Movie }>('/api/v1/scrape', {
			method: 'POST',
			body: JSON.stringify(request),
		});
		return response.movie;
	}

	async getMovie(id: string): Promise<Movie> {
		const response = await this.request<{ movie: Movie }>(`/api/v1/movies/${id}`);
		return response.movie;
	}

	async listMovies(limit?: number, offset?: number): Promise<{ movies: Movie[]; count: number }> {
		const params = new URLSearchParams();
		if (limit) params.set('limit', limit.toString());
		if (offset) params.set('offset', offset.toString());
		const query = params.toString() ? `?${params}` : '';
		return this.request(`/api/v1/movies${query}`);
	}
}
