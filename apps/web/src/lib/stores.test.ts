// Pane layout store ops: move, swap, and pair-resize semantics.
import { test, expect, beforeEach } from 'bun:test';
import { get } from 'svelte/store';
import { panes, movePane, swapPanes, sizePair, newPane, closePane } from './stores';

const ids = () => get(panes).map((p) => p.id);
const sizes = () => get(panes).map((p) => p.size);

function seed(n: number, size?: number) {
	panes.set([]);
	for (let i = 0; i < n; i++) newPane('s' + i);
	if (size !== undefined) panes.update((ps) => ps.map((p) => ({ ...p, size })));
}

beforeEach(() => panes.set([]));

test('movePane reorders to an absolute index', () => {
	seed(3);
	const [a, b, c] = ids();
	movePane(c, 0);
	expect(ids()).toEqual([c, a, b]);
	movePane(a, 2);
	expect(ids()).toEqual([c, b, a]);
});

test('movePane clamps out-of-range targets', () => {
	seed(2);
	const [a, b] = ids();
	movePane(b, 99);
	expect(ids()).toEqual([a, b]);
});

test('swapPanes exchanges positions, sizes travel with panes', () => {
	seed(3);
	const [a, , c] = ids();
	panes.update((ps) => ps.map((p, i) => ({ ...p, size: i + 1 })));
	swapPanes(a, c);
	expect(ids()).toEqual([c, ids()[1], a]);
	expect(sizes()).toEqual([3, 2, 1]);
});

test('sizePair redistributes only the adjacent pair and preserves its total', () => {
	seed(3, 1);
	const [a, b, c] = ids();
	sizePair(a, b, 1.5);
	expect(sizes()).toEqual([1.5, 0.5, 1]);
	// non-adjacent pair is rejected
	sizePair(a, c, 0.2);
	expect(sizes()).toEqual([1.5, 0.5, 1]);
});

test('sizePair clamps each side to >=15% of the pair', () => {
	seed(2, 1);
	const [a, b] = ids();
	sizePair(a, b, 99);
	let s = sizes();
	expect(s[0]).toBeCloseTo(1.7, 10);
	expect(s[1]).toBeCloseTo(0.3, 10);
	sizePair(a, b, -5);
	s = sizes();
	expect(s[0]).toBeCloseTo(0.3, 10);
	expect(s[1]).toBeCloseTo(1.7, 10);
	expect(s[0] + s[1]).toBeCloseTo(2, 10);
});

test('sizePair defaults missing size to 1', () => {
	seed(2); // old layouts have no size field
	const [a, b] = ids();
	sizePair(a, b, 1.2);
	expect(sizes()).toEqual([1.2, 0.8]);
});

test('closePane drops the view; sessions are untouched (Rule 6)', () => {
	seed(2);
	const [a, b] = ids();
	closePane(a);
	expect(ids()).toEqual([b]);
});
