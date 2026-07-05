import { BaseClient, AuthClient, SystemClient, getAPIBaseURL } from './clients/common';
import type { DesktopUpgradeRequest, DesktopUpgradeResponse } from './types';
import { JobClient, FileClient, ScraperClient } from './clients/jobs';
import { ActressClient } from './clients/actress';
import { ReplacementClient } from './clients/replacement';
import { HistoryClient, JobsClient, EventsClient } from './clients/history';
import { ConfigClient } from './clients/config';

/**
 * APIClient composes domain-scoped sub-clients for all API endpoints.
 *
 * The sub-clients are:
 * - auth: Authentication (login, setup, logout)
 * - system: Health, version, CWD, preview images
 * - jobs: Batch job lifecycle (scrape, organize, update, rescrape, exclude)
 * - files: Filesystem scanning, browsing, path autocomplete
 * - scrapers: Standalone scrape, scraper metadata, movie queries
 * - actresses: Actress CRUD, merge, search, import/export
 * - replacements: Genre/word replacement CRUD, import/export
 * - history: History record queries, stats, deletion
 * - organizedJobs: Organized-jobs listing and revert
 * - events: Event log queries, stats, deletion
 * - config: Configuration, proxy testing, translation models
 *
 * All methods on APIClient delegate to the corresponding sub-client,
 * preserving backward compatibility for existing consumers.
 */
class APIClient {
	private inner: BaseClient;

	// Domain sub-clients
	readonly auth: AuthClient;
	readonly system: SystemClient;
	readonly jobs: JobClient;
	readonly files: FileClient;
	readonly scrapers: ScraperClient;
	readonly actresses: ActressClient;
	readonly replacements: ReplacementClient;
	readonly history: HistoryClient;
	readonly organizedJobs: JobsClient;
	readonly events: EventsClient;
	readonly config: ConfigClient;

	constructor(baseURL?: string) {
		const url = baseURL ?? getAPIBaseURL();
		this.inner = new BaseClient(url);

		// Initialize domain sub-clients
		this.auth = new AuthClient(url);
		this.system = new SystemClient(url);
		this.jobs = new JobClient(url);
		this.files = new FileClient(url);
		this.scrapers = new ScraperClient(url);
		this.actresses = new ActressClient(url);
		this.replacements = new ReplacementClient(url);
		this.history = new HistoryClient(url);
		this.organizedJobs = new JobsClient(url);
		this.events = new EventsClient(url);
		this.config = new ConfigClient(url);
	}

	// Shared request method — delegates to BaseClient
	public async request<T>(endpoint: string, options?: RequestInit): Promise<T> {
		return this.inner.request<T>(endpoint, options);
	}

	// --- Backward-compatible facade methods ---
	// These delegate to the domain sub-clients so existing consumers
	// (routes, stores, components) don't need to change.

	// Health & auth
	async health() {
		return this.system.health();
	}
	async getAuthStatus() {
		return this.auth.getAuthStatus();
	}
	async setupAuth(credentials: Parameters<AuthClient['setupAuth']>[0]) {
		return this.auth.setupAuth(credentials);
	}
	async loginAuth(credentials: Parameters<AuthClient['loginAuth']>[0]) {
		return this.auth.loginAuth(credentials);
	}
	async logoutAuth() {
		return this.auth.logoutAuth();
	}

	// System
	getPreviewImageURL(imageURL: string) {
		return this.system.getPreviewImageURL(imageURL);
	}
	async getCurrentWorkingDirectory() {
		return this.system.getCurrentWorkingDirectory();
	}
	async getVersionStatus() {
		return this.system.getVersionStatus();
	}
	async checkVersion() {
		return this.system.checkVersion();
	}

	// Desktop self-upgrade (desktop builds only; 404 otherwise).
	async upgradeDesktop(request: DesktopUpgradeRequest) {
		return this.request<DesktopUpgradeResponse>('/api/v1/desktop/upgrade', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	// File operations
	async scan(request: Parameters<FileClient['scan']>[0]) {
		return this.files.scan(request);
	}
	async browse(request: Parameters<FileClient['browse']>[0]) {
		return this.files.browse(request);
	}
	async autocompletePath(request: Parameters<FileClient['autocompletePath']>[0]) {
		return this.files.autocompletePath(request);
	}

	// Batch jobs
	async batchScrape(request: Parameters<JobClient['batchScrape']>[0]) {
		return this.jobs.batchScrape(request);
	}
	async getBatchJob(jobId: string, includeData?: boolean) {
		return this.jobs.getBatchJob(jobId, includeData);
	}
	async cancelBatchJob(jobId: string) {
		return this.jobs.cancelBatchJob(jobId);
	}
	async deleteBatchJob(jobId: string) {
		return this.jobs.deleteBatchJob(jobId);
	}
	async listBatchJobs() {
		return this.jobs.listBatchJobs();
	}
	async updateBatchMovie(
		jobId: string,
		resultId: string,
		movie: Parameters<JobClient['updateBatchMovie']>[2],
	) {
		return this.jobs.updateBatchMovie(jobId, resultId, movie);
	}
	async updateBatchMoviePosterCrop(
		jobId: string,
		resultId: string,
		crop: Parameters<JobClient['updateBatchMoviePosterCrop']>[2],
	) {
		return this.jobs.updateBatchMoviePosterCrop(jobId, resultId, crop);
	}
	async updateBatchMoviePosterFromURL(
		jobId: string,
		resultId: string,
		request: Parameters<JobClient['updateBatchMoviePosterFromURL']>[2],
	) {
		return this.jobs.updateBatchMoviePosterFromURL(jobId, resultId, request);
	}
	async getBatchMovieSources(jobId: string, resultId: string) {
		return this.jobs.getBatchMovieSources(jobId, resultId);
	}
	async overrideBatchMovieField(
		jobId: string,
		resultId: string,
		request: Parameters<JobClient['overrideBatchMovieField']>[2],
	) {
		return this.jobs.overrideBatchMovieField(jobId, resultId, request);
	}
	async excludeBatchMovie(jobId: string, resultId: string) {
		return this.jobs.excludeBatchMovie(jobId, resultId);
	}
	async batchExcludeMovies(jobId: string, request: Parameters<JobClient['batchExcludeMovies']>[1]) {
		return this.jobs.batchExcludeMovies(jobId, request);
	}
	async bulkRescrapeMovies(jobId: string, request: Parameters<JobClient['bulkRescrapeMovies']>[1]) {
		return this.jobs.bulkRescrapeMovies(jobId, request);
	}
	async organizeBatchJob(jobId: string, request: Parameters<JobClient['organizeBatchJob']>[1]) {
		return this.jobs.organizeBatchJob(jobId, request);
	}
	async updateBatchJob(jobId: string, request?: Parameters<JobClient['updateBatchJob']>[1]) {
		return this.jobs.updateBatchJob(jobId, request);
	}
	async previewOrganize(
		jobId: string,
		resultId: string,
		request: Parameters<JobClient['previewOrganize']>[2],
	) {
		return this.jobs.previewOrganize(jobId, resultId, request);
	}
	async rescrapeBatchMovie(
		jobId: string,
		resultId: string,
		req: Parameters<JobClient['rescrapeBatchMovie']>[2],
	) {
		return this.jobs.rescrapeBatchMovie(jobId, resultId, req);
	}

	// Scrapers & movies
	async getAvailableScrapers() {
		return this.scrapers.getAvailableScrapers();
	}
	async getScrapers() {
		return this.scrapers.getScrapers();
	}
	async rescrapeMovie(id: string, req: Parameters<ScraperClient['rescrapeMovie']>[1]) {
		return this.scrapers.rescrapeMovie(id, req);
	}
	async scrapeMovie(input: string, options?: Parameters<ScraperClient['scrapeMovie']>[1]) {
		return this.scrapers.scrapeMovie(input, options);
	}
	async getMovie(id: string) {
		return this.scrapers.getMovie(id);
	}
	async listMovies(limit?: number, offset?: number) {
		return this.scrapers.listMovies(limit, offset);
	}

	// Actresses
	async listActresses(params?: Parameters<ActressClient['listActresses']>[0]) {
		return this.actresses.listActresses(params);
	}
	async getActress(id: number) {
		return this.actresses.getActress(id);
	}
	async createActress(request: Parameters<ActressClient['createActress']>[0]) {
		return this.actresses.createActress(request);
	}
	async updateActress(id: number, request: Parameters<ActressClient['updateActress']>[1]) {
		return this.actresses.updateActress(id, request);
	}
	async deleteActress(id: number) {
		return this.actresses.deleteActress(id);
	}
	async previewActressMerge(request: Parameters<ActressClient['previewActressMerge']>[0]) {
		return this.actresses.previewActressMerge(request);
	}
	async mergeActresses(request: Parameters<ActressClient['mergeActresses']>[0]) {
		return this.actresses.mergeActresses(request);
	}
	async exportActresses() {
		return this.actresses.exportActresses();
	}
	async importActresses(request: Parameters<ActressClient['importActresses']>[0]) {
		return this.actresses.importActresses(request);
	}

	// Replacements
	async listGenreReplacements(params?: Parameters<ReplacementClient['listGenreReplacements']>[0]) {
		return this.replacements.listGenreReplacements(params);
	}
	async createGenreReplacement(
		request: Parameters<ReplacementClient['createGenreReplacement']>[0],
	) {
		return this.replacements.createGenreReplacement(request);
	}
	async deleteGenreReplacement(id: number) {
		return this.replacements.deleteGenreReplacement(id);
	}
	async updateGenreReplacement(
		request: Parameters<ReplacementClient['updateGenreReplacement']>[0],
	) {
		return this.replacements.updateGenreReplacement(request);
	}
	async exportGenreReplacements() {
		return this.replacements.exportGenreReplacements();
	}
	async importGenreReplacements(
		request: Parameters<ReplacementClient['importGenreReplacements']>[0],
	) {
		return this.replacements.importGenreReplacements(request);
	}
	async listWordReplacements(params?: Parameters<ReplacementClient['listWordReplacements']>[0]) {
		return this.replacements.listWordReplacements(params);
	}
	async createWordReplacement(request: Parameters<ReplacementClient['createWordReplacement']>[0]) {
		return this.replacements.createWordReplacement(request);
	}
	async updateWordReplacement(request: Parameters<ReplacementClient['updateWordReplacement']>[0]) {
		return this.replacements.updateWordReplacement(request);
	}
	async deleteWordReplacement(id: number) {
		return this.replacements.deleteWordReplacement(id);
	}
	async exportWordReplacements() {
		return this.replacements.exportWordReplacements();
	}
	async importWordReplacements(
		request: Parameters<ReplacementClient['importWordReplacements']>[0],
	) {
		return this.replacements.importWordReplacements(request);
	}

	// History
	async getHistory(params?: Parameters<HistoryClient['getHistory']>[0]) {
		return this.history.getHistory(params);
	}
	async getHistoryStats() {
		return this.history.getHistoryStats();
	}
	async deleteHistory(id: number) {
		return this.history.deleteHistory(id);
	}
	async deleteHistoryBulk(params: Parameters<HistoryClient['deleteHistoryBulk']>[0]) {
		return this.history.deleteHistoryBulk(params);
	}

	// Organized jobs
	async listOrganizedJobs(params?: Parameters<JobsClient['listOrganizedJobs']>[0]) {
		return this.organizedJobs.listOrganizedJobs(params);
	}
	async getJob(jobId: string) {
		return this.organizedJobs.getJob(jobId);
	}
	async getJobOperations(jobId: string) {
		return this.organizedJobs.getJobOperations(jobId);
	}
	async revertBatchJob(jobId: string) {
		return this.organizedJobs.revertBatchJob(jobId);
	}
	async revertJobOperation(jobId: string, movieId: string) {
		return this.organizedJobs.revertJobOperation(jobId, movieId);
	}

	// Events
	async listEvents(params?: Parameters<EventsClient['listEvents']>[0]) {
		return this.events.listEvents(params);
	}
	async getEventStats() {
		return this.events.getEventStats();
	}
	async deleteEvents(params: Parameters<EventsClient['deleteEvents']>[0]) {
		return this.events.deleteEvents(params);
	}

	// Config
	async getConfig() {
		return this.config.getConfig();
	}
	async testProxy(request: Parameters<ConfigClient['testProxy']>[0]) {
		return this.config.testProxy(request);
	}
	async getTranslationModels(request: Parameters<ConfigClient['getTranslationModels']>[0]) {
		return this.config.getTranslationModels(request);
	}
	async getDeepLUsage(request: Parameters<ConfigClient['getDeepLUsage']>[0]) {
		return this.config.getDeepLUsage(request);
	}
}

export const apiClient = new APIClient();
export default apiClient;
