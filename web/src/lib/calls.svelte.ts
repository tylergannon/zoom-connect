import { api, errorMessage } from './api';
import type { Call } from './types';

const ANNOUNCEMENTS = ['call.scheduled', 'call.started', 'call.shared', 'call.ended', 'call.cancelled'];

/** The calls the signed-in person can see, kept current from /api/stream. */
class CallIndex {
	calls = $state<Call[]>([]);
	status = $state<'loading' | 'ready' | 'error'>('loading');
	error = $state('');
	/** Wall clock for "3 min ago" labels, updated every 30 seconds. */
	now = $state(Date.now());
	live = $derived(this.calls.filter((c) => c.status === 'live'));

	#pending: Promise<void> | null = null;
	#again = false;

	refresh(): Promise<void> {
		if (this.#pending) {
			this.#again = true;
			return this.#pending;
		}
		this.#pending = (async () => {
			try {
				do {
					this.#again = false;
					this.calls = await api<Call[]>('/api/calls');
					this.status = 'ready';
					this.error = '';
				} while (this.#again);
			} catch (e) {
				if (this.status !== 'ready') this.status = 'error';
				this.error = errorMessage(e);
			} finally {
				this.#pending = null;
			}
		})();
		return this.#pending;
	}

	/** Start listening; returns the function that stops. Called once by the app shell. */
	start() {
		void this.refresh();
		const source = new EventSource('/api/stream');
		const update = () => void this.refresh();
		for (const type of ANNOUNCEMENTS) source.addEventListener(type, update);
		source.onmessage = update;
		const tick = setInterval(() => {
			this.now = Date.now();
			// The newest line on a live card only changes through a refresh.
			if (this.live.length) void this.refresh();
		}, 30_000);
		return () => {
			source.close();
			clearInterval(tick);
		};
	}
}

export const callIndex = new CallIndex();
