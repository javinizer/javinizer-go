import { vi } from 'vitest';

// Minimal $app/navigation stub for component tests (vitest config aliases
// $app/navigation here). goto is a vi.fn so tests can assert redirects / Back.
export const goto = vi.fn();
export const invalidate = vi.fn();
export const invalidateAll = vi.fn();
export const prefetch = vi.fn();
export const beforeNavigate = vi.fn();
export const afterNavigate = vi.fn();
export const onNavigate = vi.fn();
export function applyAction() {}
export function destroyUrl() {}
