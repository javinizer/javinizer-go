import type {
	Config,
	ProxyTestRequest,
	ProxyTestResponse,
	TranslationModelsRequest,
	TranslationModelsResponse,
	DeepLUsageRequest,
	DeepLUsageResponse,
	SecurityUpdateRequest,
	SecurityUpdateResponse,
} from '../types';
import { BaseClient } from './common';

// ConfigClient handles configuration, proxy testing, and translation model discovery.
export class ConfigClient extends BaseClient {
	async getConfig(): Promise<Config> {
		return this.request<Config>('/api/v1/config');
	}

	async testProxy(request: ProxyTestRequest): Promise<ProxyTestResponse> {
		return this.request<ProxyTestResponse>('/api/v1/proxy/test', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async getTranslationModels(
		request: TranslationModelsRequest,
	): Promise<TranslationModelsResponse> {
		return this.request<TranslationModelsResponse>('/api/v1/translation/models', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async getDeepLUsage(request: DeepLUsageRequest): Promise<DeepLUsageResponse> {
		return this.request<DeepLUsageResponse>('/api/v1/translation/deepl/usage', {
			method: 'POST',
			body: JSON.stringify(request),
		});
	}

	async updateSecurityConfig(request: SecurityUpdateRequest): Promise<SecurityUpdateResponse> {
		return this.request<SecurityUpdateResponse>('/api/v1/config/security', {
			method: 'PUT',
			body: JSON.stringify(request),
		});
	}
}
