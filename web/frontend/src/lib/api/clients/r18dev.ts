import type { DumpStatus, DumpSearchResult } from '../types';
import { BaseClient } from './common';

export class R18DevClient extends BaseClient {
	async getDumpStatus(): Promise<DumpStatus> {
		return this.request<DumpStatus>('/api/v1/r18dev/dump/status');
	}

	async downloadDump(updateOnly = false): Promise<void> {
		await this.request<void>(`/api/v1/r18dev/dump/${updateOnly ? 'update' : 'download'}`, {
			method: 'POST',
		});
	}

	async searchDump(query: string): Promise<DumpSearchResult> {
		const params = new URLSearchParams({ q: query });
		return this.request<DumpSearchResult>(`/api/v1/r18dev/dump/search?${params.toString()}`);
	}

	async clearDump(): Promise<void> {
		await this.request<void>('/api/v1/r18dev/dump', { method: 'DELETE' });
	}

	// uploadDump posts a user-selected dump file (.db sidecar or .sql.gz raw
	// dump) via multipart. The receive is synchronous (202 once staged) and the
	// job outcome arrives via the dump progress channel + status endpoint.
	async uploadDump(file: File): Promise<{ message: string }> {
		const form = new FormData();
		form.append('file', file, file.name);
		return this.request<{ message: string }>('/api/v1/r18dev/dump/upload', {
			method: 'POST',
			body: form,
		});
	}
}
