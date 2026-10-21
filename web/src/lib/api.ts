import type { Person } from './types';

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

/** A sentence to show a person when a request fails. */
export function errorMessage(error: unknown) {
	if (error instanceof APIError) {
		if (error.status === 401) return 'Your session ended. Sign in again to continue.';
		if (error.status >= 500) return 'The server had a problem. Try again in a moment.';
		const text = error.message.trim();
		return text ? text[0].toUpperCase() + text.slice(1) + (/[.!?]$/.test(text) ? '' : '.') : 'That didn’t work. Try again.';
	}
	if (error instanceof TypeError) return 'We couldn’t reach the server. Check your connection, then try again.';
	return 'Something went wrong. Try again.';
}

export function initials(name = '') {
	return name.split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]).join('').toUpperCase() || '?';
}

const dayKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;

/** "Today", "Yesterday", "Tomorrow", or a date such as "Mon, Oct 19". */
export function dayLabel(value: string | Date, now = new Date()) {
	const d = new Date(value);
	const offset = (n: number) => { const x = new Date(now); x.setDate(x.getDate() + n); return dayKey(x); };
	if (dayKey(d) === offset(0)) return 'Today';
	if (dayKey(d) === offset(-1)) return 'Yesterday';
	if (dayKey(d) === offset(1)) return 'Tomorrow';
	return new Intl.DateTimeFormat(undefined, {
		weekday: 'short', month: 'short', day: 'numeric',
		year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric'
	}).format(d);
}

export function timeOfDay(value: string | number | Date, seconds = false) {
	return new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit', second: seconds ? '2-digit' : undefined }).format(new Date(value));
}

/** "Today at 2:04 PM". */
export function when(value?: string | null) {
	if (!value) return '';
	return `${dayLabel(value)} at ${timeOfDay(value)}`;
}

/** Whole minutes in words: "48 min", "1 hr 5 min", "Under a minute". */
export function minutes(start?: string | null, end?: string | null, now = Date.now()) {
	if (!start) return '';
	const total = Math.max(0, Math.floor(((end ? new Date(end).getTime() : now) - new Date(start).getTime()) / 60000));
	if (total < 1) return 'Under a minute';
	const h = Math.floor(total / 60), m = total % 60;
	return h ? `${h} hr${m ? ` ${m} min` : ''}` : `${m} min`;
}

/** "Started 3 min ago" style phrase. */
export function ago(value: string, now = Date.now()) {
	const mins = Math.floor((now - new Date(value).getTime()) / 60000);
	if (mins < 1) return 'just now';
	if (mins < 60) return `${mins} min ago`;
	const hours = Math.floor(mins / 60);
	if (hours < 24) return `${hours} hr ago`;
	return when(value);
}

/** Offset from the call start as mm:ss or h:mm:ss. */
export function clock(ms: number) {
	const s = Math.max(0, Math.floor(ms / 1000));
	const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = s % 60;
	const pad = (n: number) => String(n).padStart(2, '0');
	return h ? `${h}:${pad(m)}:${pad(sec)}` : `${pad(m)}:${pad(sec)}`;
}

/** "Dana Kim, Priya Shah, Leo Park and 2 more". */
export function names(people: Person[], max = 3) {
	const list = people.map((p) => p.name);
	if (list.length === 0) return '';
	if (list.length === 1) return list[0];
	if (list.length <= max) return `${list.slice(0, -1).join(', ')} and ${list.at(-1)}`;
	return `${list.slice(0, max).join(', ')} and ${list.length - max} more`;
}

export function plural(n: number, one: string, many = `${one}s`) {
	return `${n} ${n === 1 ? one : many}`;
}

export async function copyText(text: string) {
	try { await navigator.clipboard.writeText(text); return true; }
	catch { return false; }
}
