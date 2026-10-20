import { describe, expect, it } from 'vite-plus/test';
import { buildTurns, findMatches, lineCount } from './transcript';
import type { EventItem } from '#lib/types';

const dana = { id: 'u1', name: 'Dana Kim' };
const priya = { id: 'u2', name: 'Priya Shah' };
let seq = 0;
const ev = (kind: string, ts: number, participant?: typeof dana, data: EventItem['data'] = {}): EventItem => ({ seq: ++seq, ts, kind, participant, data });
const say = (who: typeof dana, ts: number, text: string, final = true) => ev('transcript', ts, who, { text, final });

describe('buildTurns', () => {
	it('merges consecutive lines from one speaker and keeps chat separate', () => {
		const turns = buildTurns([
			ev('stream.started', 0),
			say(dana, 1_000, 'Okay, we are recording.'),
			say(dana, 5_000, 'Let us start.'),
			say(priya, 9_000, 'The export is done.'),
			ev('chat', 10_000, dana, { text: 'Spec is in the doc.' }),
			say(priya, 12_000, 'Leo, can you review it?')
		], 'transcript');
		expect(turns.map((t) => t.type)).toEqual(['system', 'speech', 'speech', 'chat', 'speech']);
		expect(turns[0]).toMatchObject({ text: 'Transcription started' });
		expect(turns[1]).toMatchObject({ who: 'Dana Kim', paras: [{ text: 'Okay, we are recording.' }, { text: 'Let us start.' }] });
		expect(lineCount(turns)).toBe(5);
	});

	it('starts a new turn after a long pause', () => {
		const turns = buildTurns([say(dana, 0, 'One.'), say(dana, 61_000, 'Two.')], 'transcript');
		expect(turns).toHaveLength(2);
	});

	it('drops a partial line once the same speaker says more', () => {
		const turns = buildTurns([say(dana, 0, 'Jordan, you had', false), say(dana, 2_000, 'Jordan, you had numbers.'), say(priya, 3_000, 'I think', false)], 'transcript');
		const paras = turns.flatMap((t) => (t.type === 'system' ? [] : t.paras));
		expect(paras.map((p) => [p.text, p.partial])).toEqual([['Jordan, you had numbers.', false], ['I think', true]]);
	});

	it('notes a screen share once per run of frames', () => {
		const turns = buildTurns([ev('screen.frame', 0, priya), ev('screen.frame', 1_000, priya), ev('screen.frame', 120_000, priya)], 'transcript');
		expect(turns.map((t) => (t.type === 'system' ? t.text : ''))).toEqual(['Priya Shah shared a screen', 'Priya Shah shared a screen']);
	});

	it('shows only chat messages in chat mode', () => {
		const turns = buildTurns([say(dana, 0, 'Hello'), ev('chat', 1, priya, { text: 'Hi' }), ev('stream.stopped', 2)], 'chat');
		expect(turns).toEqual([expect.objectContaining({ type: 'chat', who: 'Priya Shah' })]);
	});
});

describe('findMatches', () => {
	it('numbers matches across paragraphs in reading order', () => {
		const turns = buildTurns([say(dana, 0, 'Export first, then export again.'), say(priya, 70_000, 'EXPORT done')], 'transcript');
		const { segments, total } = findMatches(turns, 'export');
		expect(total).toBe(3);
		const first = [...segments.values()][0];
		expect(first).toEqual([{ text: 'Export', match: 0 }, { text: ' first, then ' }, { text: 'export', match: 1 }, { text: ' again.' }]);
		expect(findMatches(turns, '  ').total).toBe(0);
	});
});
