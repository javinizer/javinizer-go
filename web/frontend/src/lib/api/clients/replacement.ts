import type {
	GenreReplacement,
	GenreReplacementListResponse,
	GenreReplacementCreateRequest,
	GenreReplacementUpdateRequest,
	IgnoredGenresResponse,
	FavoriteGenresResponse,
	GenreListUpdateRequest,
	GenreAddRequest,
	WordReplacement,
	WordReplacementListResponse,
	WordReplacementCreateRequest,
	WordReplacementUpdateRequest,
	ImportResponse,
	GenreReplacementsImportRequest,
	WordReplacementsImportRequest,
} from '../types';
import { BaseClient } from './common';

// ReplacementClient handles genre and word replacement CRUD, import, and export.
export class ReplacementClient extends BaseClient {
	// --- Genre replacements ---

	async listGenreReplacements(params?: {
		limit?: number;
		offset?: number;
	}): Promise<GenreReplacementListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<GenreReplacementListResponse>(`/api/v1/genres/replacements${query}`);
	}

	async createGenreReplacement(request: GenreReplacementCreateRequest): Promise<GenreReplacement> {
		return this.request<GenreReplacement>('/api/v1/genres/replacements', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async deleteGenreReplacement(id: number): Promise<void> {
		await this.request(`/api/v1/genres/replacements?id=${id}`, { method: 'DELETE' });
	}

	async updateGenreReplacement(request: GenreReplacementUpdateRequest): Promise<GenreReplacement> {
		return this.request<GenreReplacement>('/api/v1/genres/replacements', {
			method: 'PUT',
			body: JSON.stringify(request),
		});
	}

	async exportGenreReplacements(): Promise<GenreReplacement[]> {
		return this.request<GenreReplacement[]>('/api/v1/genres/replacements/export', {
			method: 'GET',
		});
	}

	async importGenreReplacements(request: GenreReplacementsImportRequest): Promise<ImportResponse> {
		return this.request<ImportResponse>('/api/v1/genres/replacements/import', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	// --- Ignore genres (config-backed) ---

	async listIgnoredGenres(): Promise<IgnoredGenresResponse> {
		return this.request<IgnoredGenresResponse>('/api/v1/genres/ignored');
	}

	async addIgnoredGenre(request: GenreAddRequest): Promise<IgnoredGenresResponse> {
		return this.request<IgnoredGenresResponse>('/api/v1/genres/ignored', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async replaceIgnoredGenres(request: GenreListUpdateRequest): Promise<IgnoredGenresResponse> {
		return this.request<IgnoredGenresResponse>('/api/v1/genres/ignored', {
			method: 'PUT',
			body: JSON.stringify(request),
		});
	}

	async deleteIgnoredGenre(genre: string): Promise<IgnoredGenresResponse> {
		return this.request<IgnoredGenresResponse>(
			`/api/v1/genres/ignored?genre=${encodeURIComponent(genre)}`,
			{ method: 'DELETE' }
		);
	}

	// --- Favorite genres (config-backed) ---

	async listFavoriteGenres(): Promise<FavoriteGenresResponse> {
		return this.request<FavoriteGenresResponse>('/api/v1/genres/favorites');
	}

	async addFavoriteGenre(request: GenreAddRequest): Promise<FavoriteGenresResponse> {
		return this.request<FavoriteGenresResponse>('/api/v1/genres/favorites', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async replaceFavoriteGenres(request: GenreListUpdateRequest): Promise<FavoriteGenresResponse> {
		return this.request<FavoriteGenresResponse>('/api/v1/genres/favorites', {
			method: 'PUT',
			body: JSON.stringify(request),
		});
	}

	async deleteFavoriteGenre(genre: string): Promise<FavoriteGenresResponse> {
		return this.request<FavoriteGenresResponse>(
			`/api/v1/genres/favorites?genre=${encodeURIComponent(genre)}`,
			{ method: 'DELETE' }
		);
	}

	// --- Word replacements ---

	async listWordReplacements(params?: {
		limit?: number;
		offset?: number;
	}): Promise<WordReplacementListResponse> {
		const queryParams = new URLSearchParams();
		if (params?.limit) queryParams.set('limit', params.limit.toString());
		if (params?.offset) queryParams.set('offset', params.offset.toString());
		const query = queryParams.toString() ? `?${queryParams}` : '';
		return this.request<WordReplacementListResponse>(`/api/v1/words/replacements${query}`);
	}

	async createWordReplacement(request: WordReplacementCreateRequest): Promise<WordReplacement> {
		return this.request<WordReplacement>('/api/v1/words/replacements', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async updateWordReplacement(request: WordReplacementUpdateRequest): Promise<WordReplacement> {
		return this.request<WordReplacement>('/api/v1/words/replacements', {
			method: 'PUT',
			body: JSON.stringify(request),
		});
	}

	async deleteWordReplacement(id: number): Promise<void> {
		await this.request(`/api/v1/words/replacements?id=${id}`, { method: 'DELETE' });
	}

	async exportWordReplacements(): Promise<WordReplacement[]> {
		return this.request<WordReplacement[]>('/api/v1/words/replacements/export', { method: 'GET' });
	}

	async importWordReplacements(request: WordReplacementsImportRequest): Promise<ImportResponse> {
		return this.request<ImportResponse>('/api/v1/words/replacements/import', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}
}
