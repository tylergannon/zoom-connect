export class APIError extends Error {
	constructor(public status: number, message: string, public body?: any) { super(message); }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
	const response = await fetch(path, {
		...init,
		credentials: 'same-origin',
		headers: { ...(init.body ? { 'Content-Type': 'application/json' } : {}), ...init.headers }
	});
	if (!response.ok) {
		let body: any;
		try { body = await response.json(); } catch { body = undefined; }
		throw new APIError(response.status, body?.error || response.statusText || 'Request failed', body);
	}
	if (response.status === 204) return undefined as T;
	return response.json() as Promise<T>;
}

export function errorMessage(error: unknown) {
	return error instanceof Error ? error.message : 'Something went wrong. Please try again.';
}

export function initials(name = '') { return name.split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]).join('').toUpperCase() || '?'; }
export function relativeDate(value?: string) {
	if (!value) return '—';
	return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value));
}
export function duration(start: string, end?: string) {
	const seconds = Math.max(0, Math.floor(((end ? new Date(end).getTime() : Date.now()) - new Date(start).getTime()) / 1000));
	return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}
