import { browser } from '$app/environment';
import { getLocale } from '$lib/paraglide/runtime';

export function activeLocale(): string {
	if (!browser) return 'en';
	return getLocale() ?? 'en';
}

function toDate(input: Date | string | number): Date | null {
	if (input instanceof Date) return Number.isNaN(input.getTime()) ? null : input;
	if (typeof input === 'number' || typeof input === 'string') {
		const d = new Date(input);
		return Number.isNaN(d.getTime()) ? null : d;
	}
	return null;
}

export function formatDate(
	input: Date | string | number,
	options?: Intl.DateTimeFormatOptions
): string {
	const date = toDate(input);
	if (!date) return '';
	const opts = options ?? { dateStyle: 'medium' };
	try {
		return new Intl.DateTimeFormat(activeLocale(), opts).format(date);
	} catch {
		return new Intl.DateTimeFormat('en', opts).format(date);
	}
}

export function formatDateTime(
	input: Date | string | number,
	options?: Intl.DateTimeFormatOptions
): string {
	return formatDate(input, options ?? { dateStyle: 'medium', timeStyle: 'short' });
}

export function formatNumber(value: number, options?: Intl.NumberFormatOptions): string {
	try {
		return new Intl.NumberFormat(activeLocale(), options).format(value);
	} catch {
		return new Intl.NumberFormat('en', options).format(value);
	}
}

export function formatRelativeTime(input: Date | string | number): string {
	const date = toDate(input);
	if (!date) return '';
	const diffMs = date.getTime() - Date.now();
	const seconds = Math.round(diffMs / 1000);
	const abs = Math.abs(seconds);
	const units: Array<{ unit: Intl.RelativeTimeFormatUnit; limit: number }> = [
		{ unit: 'second', limit: 60 },
		{ unit: 'minute', limit: 3600 },
		{ unit: 'hour', limit: 86400 },
		{ unit: 'day', limit: 2592000 },
		{ unit: 'month', limit: 31536000 },
		{ unit: 'year', limit: Infinity }
	];
	let unit: Intl.RelativeTimeFormatUnit = 'second';
	let value = seconds;
	for (const u of units) {
		if (abs < u.limit) {
			unit = u.unit;
			break;
		}
		value = seconds;
	}
	switch (unit) {
		case 'minute':
			value = Math.round(seconds / 60);
			break;
		case 'hour':
			value = Math.round(seconds / 3600);
			break;
		case 'day':
			value = Math.round(seconds / 86400);
			break;
		case 'month':
			value = Math.round(seconds / 2592000);
			break;
		case 'year':
			value = Math.round(seconds / 31536000);
			break;
	}
	try {
		const rtf = new Intl.RelativeTimeFormat(activeLocale(), { numeric: 'auto' });
		return rtf.format(value, unit);
	} catch {
		const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });
		return rtf.format(value, unit);
	}
}

export function formatDuration(seconds: number): string {
	if (!Number.isFinite(seconds) || seconds < 0) return '';
	const s = Math.floor(seconds);
	const hours = Math.floor(s / 3600);
	const minutes = Math.floor((s % 3600) / 60);
	const secs = s % 60;
	const parts: string[] = [];
	if (hours > 0) parts.push(`${formatNumber(hours)}h`);
	if (minutes > 0 || hours > 0) parts.push(`${formatNumber(minutes)}m`);
	parts.push(`${formatNumber(secs)}s`);
	return parts.join(' ');
}
