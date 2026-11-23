// Ref: .teos/conventions/websocket-message-catalog.md
// attach{last_seq} → hello{next_seq} → binary seq-frames / ctl frames.
// Reconnects with the last seen seq — the resync contract.
export type SessionEvents = {
	data: (payload: Uint8Array) => void;
	eof: (exitCode: number) => void;
	error: (code: string, message: string) => void;
	open: (nextSeq: number) => void;
	close: () => void;
};

const BACKOFF = [500, 1000, 2000, 4000, 8000, 15000];

export class SessionClient {
	private ws: WebSocket | null = null;
	private lastSeq = 0;
	private tries = 0;
	private closed = false;
	private reconnectTimer: ReturnType<typeof setTimeout> | null = null;

	constructor(
		private sessionId: string,
		private ev: SessionEvents
	) {}

	connect() {
		if (this.closed) return;
		const proto = location.protocol === 'https:' ? 'wss' : 'ws';
		const ws = new WebSocket(`${proto}://${location.host}/ws/session/${this.sessionId}`);
		ws.binaryType = 'arraybuffer';
		this.ws = ws;

		ws.onopen = () => {
			ws.send(JSON.stringify({ type: 'attach', last_seq: this.lastSeq }));
		};
		ws.onmessage = (e) => {
			if (e.data instanceof ArrayBuffer) {
				const buf = new Uint8Array(e.data);
				if (buf.length < 8) return;
				const seq = Number(
					buf.slice(0, 8).reduce((v, b) => v * 256n + BigInt(b), 0n)
				);
				if (seq > this.lastSeq) {
					this.lastSeq = seq;
					this.ev.data(buf.subarray(8));
				}
				// seq <= lastSeq: boundary duplicate from snapshot+subscribe — drop.
				return;
			}
			const msg = JSON.parse(e.data as string);
			switch (msg.type) {
				case 'hello':
					this.tries = 0;
					this.ev.open(msg.next_seq);
					break;
				case 'eof':
					this.closed = true;
					this.ev.eof(msg.exit_code);
					break;
				case 'error':
					if (msg.code === 'resync_overflow') {
						this.lastSeq = 0; // snapshot follows
					}
					this.ev.error(msg.code, msg.message);
					break;
			}
		};
		ws.onclose = () => {
			this.ev.close();
			if (this.closed) return;
			const delay = BACKOFF[Math.min(this.tries++, BACKOFF.length - 1)];
			this.reconnectTimer = setTimeout(() => this.connect(), delay);
		};
	}

	send(bytes: Uint8Array | string) {
		if (this.ws?.readyState === WebSocket.OPEN) {
			this.ws.send(typeof bytes === 'string' ? new TextEncoder().encode(bytes) : bytes);
		}
	}

	resize(cols: number, rows: number) {
		if (this.ws?.readyState === WebSocket.OPEN) {
			this.ws.send(JSON.stringify({ type: 'resize', cols, rows }));
		}
	}

	detach() {
		this.closed = true;
		if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
		try {
			this.ws?.send(JSON.stringify({ type: 'detach' }));
			this.ws?.close();
		} catch {
			/* already gone */
		}
	}
}

// Events socket — session lifecycle broadcast.
export function subscribeEvents(onEvent: (e: { type: string; session_id?: string }) => void) {
	const proto = location.protocol === 'https:' ? 'wss' : 'ws';
	let ws: WebSocket | null = null;
	let closed = false;
	let tries = 0;
	const open = () => {
		if (closed) return;
		ws = new WebSocket(`${proto}://${location.host}/ws/events`);
		ws.onmessage = (e) => {
			try {
				onEvent(JSON.parse(e.data));
			} catch {
				/* bad frame */
			}
		};
		ws.onopen = () => (tries = 0);
		ws.onclose = () => setTimeout(open, BACKOFF[Math.min(tries++, BACKOFF.length - 1)]);
	};
	open();
	return {
		close() {
			closed = true;
			ws?.close();
		}
	};
}
