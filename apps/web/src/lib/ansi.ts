// ANSI strip for the read-only scrollback preview — covers CSI, OSC
// (hyperlinks/titles), and single-char sequences; line endings stay.
const CSI = /\x1b\[[0-9;:?]*[ -/]*[@-~]/g;
const OSC = /\x1b\][^\x07\x1b]*(?:\x07|\x1b\\|$)/g;
const ESC = /\x1b[@-_]/g;
export function stripAnsi(s: string): string {
	return s.replace(OSC, '').replace(CSI, '').replace(ESC, '').replace(/\r/g, '');
}
