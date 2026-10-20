import type { EventItem } from '#lib/types';

export type Para = { seq: number; ts: number; text: string; partial: boolean };
export type Turn =
	| { type: 'speech' | 'chat'; seq: number; ts: number; who: string; paras: Para[] }
	| { type: 'system'; seq: number; ts: number; text: string };
export type Segment = { text: string; match?: number };

/** Lines from one speaker this close together read as one turn. */
const MERGE_MS = 60_000;
/** A screen frame after this long without one starts a new share. */
const SHARE_GAP_MS = 60_000;

const speaker = (e: EventItem) => e.participant?.id || e.participant?.name || 'unknown';
const isLine = (e: EventItem) => (e.kind === 'transcript' || e.kind === 'chat') && !!e.data.text;

/** Drop interim lines that a later line from the same speaker has replaced. */
function withoutReplacedPartials(events: EventItem[]) {
	const later = new Set<string>();
	const keep: EventItem[] = [];
	for (let i = events.length - 1; i >= 0; i--) {
		const e = events[i];
		if (e.kind === 'transcript') {
			if (e.data.final === false && later.has(speaker(e))) continue;
			later.add(speaker(e));
		}
		keep.push(e);
	}
	return keep.reverse();
}

function systemText(e: EventItem, started: number, lastFrame: number | null) {
	switch (e.kind) {
		case 'stream.started':
			return started === 0 ? 'Transcription started' : 'Transcription resumed';
		case 'stream.paused':
			return 'Connection to Zoom paused. Waiting for it to come back';
		case 'stream.interrupted':
			return 'Recording interrupted';
		case 'stream.stopped':
			return 'Call ended';
		case 'call.cancelled':
			return 'Scheduled call cancelled';
		case 'screen.frame':
			return lastFrame === null || e.ts - lastFrame > SHARE_GAP_MS ? `${e.participant?.name || 'Someone'} shared a screen` : null;
	}
	return null;
}

/** Group events into what a reader sees: speaker turns, chat messages and system notes. */
export function buildTurns(events: EventItem[], mode: 'transcript' | 'chat'): Turn[] {
	const turns: Turn[] = [];
	let started = 0;
	let lastFrame: number | null = null;
	for (const e of withoutReplacedPartials(events)) {
		if (mode === 'chat') {
			if (e.kind === 'chat' && e.data.text) {
				turns.push({ type: 'chat', seq: e.seq, ts: e.ts, who: e.participant?.name || 'Someone', paras: [{ seq: e.seq, ts: e.ts, text: e.data.text, partial: false }] });
			}
			continue;
		}
		if (isLine(e)) {
			const para = { seq: e.seq, ts: e.ts, text: e.data.text!, partial: e.data.final === false };
			const prev = turns.at(-1);
			if (e.kind === 'transcript' && prev?.type === 'speech' && prev.who === (e.participant?.name || 'Unknown speaker') && e.ts - prev.paras.at(-1)!.ts < MERGE_MS) {
				prev.paras.push(para);
			} else {
				turns.push({ type: e.kind === 'chat' ? 'chat' : 'speech', seq: e.seq, ts: e.ts, who: e.participant?.name || (e.kind === 'chat' ? 'Someone' : 'Unknown speaker'), paras: [para] });
			}
			continue;
		}
		const text = systemText(e, started, lastFrame);
		if (e.kind === 'stream.started') started++;
		if (e.kind === 'screen.frame') lastFrame = e.ts;
		if (text) turns.push({ type: 'system', seq: e.seq, ts: e.ts, text });
	}
	return turns;
}

/** Split each paragraph around case-insensitive matches, numbering matches in reading order. */
export function findMatches(turns: Turn[], query: string) {
	const needle = query.trim().toLowerCase();
	const segments = new Map<number, Segment[]>();
	let total = 0;
	if (!needle) return { segments, total };
	for (const t of turns) {
		if (t.type === 'system') continue;
		for (const p of t.paras) {
			const lower = p.text.toLowerCase();
			const parts: Segment[] = [];
			let from = 0;
			for (let at = lower.indexOf(needle); at !== -1; at = lower.indexOf(needle, from)) {
				if (at > from) parts.push({ text: p.text.slice(from, at) });
				parts.push({ text: p.text.slice(at, at + needle.length), match: total++ });
				from = at + needle.length;
			}
			if (parts.length) {
				if (from < p.text.length) parts.push({ text: p.text.slice(from) });
				segments.set(p.seq, parts);
			}
		}
	}
	return { segments, total };
}

export function lineCount(turns: Turn[]) {
	return turns.reduce((n, t) => n + (t.type === 'system' ? 0 : t.paras.length), 0);
}
