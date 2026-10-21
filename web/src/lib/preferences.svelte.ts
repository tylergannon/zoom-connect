// Per-device reading preferences. They live in this browser only.

export type TranscriptSize = 'default' | 'large' | 'larger';

export const transcriptSizes: { value: TranscriptSize; label: string }[] = [
	{ value: 'default', label: 'Default' },
	{ value: 'large', label: 'Large' },
	{ value: 'larger', label: 'Larger' }
];

const SIZE_KEY = 'zoom-connect-transcript-size';
const READ_KEY = 'zoom-connect-read-aloud';

function read(key: string) {
	try { return localStorage.getItem(key); } catch { return null; }
}
function write(key: string, value: string) {
	try { localStorage.setItem(key, value); } catch { /* private mode: keep it for this visit */ }
}

class Preferences {
	transcriptSize = $state<TranscriptSize>('default');
	readAloud = $state(false);

	load() {
		const size = read(SIZE_KEY);
		this.transcriptSize = size === 'large' || size === 'larger' ? size : 'default';
		this.readAloud = read(READ_KEY) === 'on';
		this.apply();
	}

	setTranscriptSize(size: TranscriptSize) {
		this.transcriptSize = size;
		write(SIZE_KEY, size);
		this.apply();
	}

	setReadAloud(on: boolean) {
		this.readAloud = on;
		write(READ_KEY, on ? 'on' : 'off');
	}

	private apply() {
		document.documentElement.dataset.transcriptSize = this.transcriptSize;
	}
}

export const preferences = new Preferences();
