// Pane pool + grid-slot ops: presets, assignment/swap, pair resizing.
import { test, expect, beforeEach } from 'bun:test';
import { get } from 'svelte/store';
import {
	panes,
	grid,
	movePane,
	swapPanes,
	assignCell,
	applyPreset,
	sizeCellPair,
	sizeRowPair,
	newPane,
	closePane,
	panels,
	sanitizePanels,
	setPanelWidth,
	resetPanelWidth,
	PANEL_MIN,
	PANEL_MAX
} from './stores';

const ids = () => get(panes).map((p) => p.id);
const occupants = () => get(grid).map((r) => r.cells.map((c) => c.paneId));
const shape = () => get(grid).map((r) => r.cells.length);

function seed(n: number, shape?: number[]) {
	panes.set([]);
	grid.set([{ cells: [{}], size: 1 }]);
	if (shape) applyPreset(shape);
	for (let i = 0; i < n; i++) newPane('s' + i);
}

beforeEach(() => {
	panes.set([]);
	grid.set([{ cells: [{}], size: 1 }]);
});

test('newPane fills the first empty slot; overflow stays pool-only', () => {
	seed(3, [2]);
	const [a, b] = ids();
	expect(occupants()).toEqual([[a, b]]);
});

test('applyPreset keeps occupants row-major and fills from the pool', () => {
	seed(4);
	const [a, b, c, d] = ids();
	expect(occupants()).toEqual([[a]]); // single slot, rest pool
	applyPreset([2, 2]);
	expect(shape()).toEqual([2, 2]);
	expect(occupants()).toEqual([
		[a, b],
		[c, d]
	]);
	applyPreset([1]);
	expect(occupants()).toEqual([[a]]); // b..d back to pool, still tabs
	expect(ids()).toHaveLength(4);
});

test('assignCell from pool displaces the occupant back to the pool', () => {
	seed(3);
	const [a, b, c] = ids();
	applyPreset([2]);
	expect(occupants()).toEqual([[a, b]]);
	assignCell(c, 0, 0);
	expect(occupants()).toEqual([[c, b]]);
	expect(ids()).toContain(a); // a still an open tab
});

test('assignCell between slots swaps occupants', () => {
	seed(3);
	const [a, b, c] = ids();
	applyPreset([3]);
	expect(occupants()).toEqual([[a, b, c]]);
	assignCell(a, 0, 2);
	expect(occupants()).toEqual([[c, b, a]]);
});

test('swapPanes on the strip also exchanges grid slots', () => {
	seed(2);
	const [a, b] = ids();
	applyPreset([1, 1]);
	expect(occupants()).toEqual([[a], [b]]);
	swapPanes(a, b);
	expect(occupants()).toEqual([[b], [a]]);
});

test('closePane vacates its slot but keeps the preset shape', () => {
	seed(2);
	const [a, b] = ids();
	applyPreset([2]);
	closePane(a);
	expect(occupants()).toEqual([[undefined, b]]);
	expect(shape()).toEqual([2]);
});

test('sizeCellPair/sizeRowPair clamp to >=15% and preserve the total', () => {
	applyPreset([2, 2]);
	sizeCellPair(0, 0, 1, 99);
	let row = get(grid)[0];
	expect(row.cells[0].size).toBeCloseTo(1.7, 10);
	expect(row.cells[1].size).toBeCloseTo(0.3, 10);
	sizeRowPair(0, 1, -5);
	expect(get(grid)[0].size).toBeCloseTo(0.3, 10);
	expect(get(grid)[1].size).toBeCloseTo(1.7, 10);
});

test('applyPreset clamps to max 4 cells', () => {
	applyPreset([6, 6]);
	expect(shape().reduce((a, b) => a + b, 0)).toBeLessThanOrEqual(8); // rows themselves clamp cells to 4
	expect(get(grid).every((r) => r.cells.length <= 4)).toBe(true);
});

test('movePane reorders pool tabs without touching slots', () => {
	seed(3);
	const [a, b, c] = ids();
	movePane(c, 0);
	expect(ids()).toEqual([c, a, b]);
});

test('sanitizePanels validates and defaults', () => {
	expect(sanitizePanels(undefined)).toEqual({
		left: { open: false, w: 300 },
		right: { open: false, w: 340 }
	});
	expect(
		sanitizePanels({ left: { open: true, w: 250 }, right: { open: 1, w: 'x' } })
	).toEqual({
		left: { open: true, w: 250 },
		right: { open: false, w: 340 }
	});
	// out-of-range widths clamp
	expect(sanitizePanels({ left: { open: false, w: 50 }, right: { open: false, w: 9999 } }))
		.toEqual({ left: { open: false, w: PANEL_MIN }, right: { open: false, w: PANEL_MAX } });
});

test('setPanelWidth clamps; resetPanelWidth restores defaults', () => {
	panels.set({ left: { open: false, w: 300 }, right: { open: false, w: 340 } });
	setPanelWidth('left', 10);
	expect(get(panels).left.w).toBe(PANEL_MIN);
	setPanelWidth('left', 99999);
	expect(get(panels).left.w).toBeLessThanOrEqual(PANEL_MAX);
	setPanelWidth('right', 500);
	resetPanelWidth('right');
	expect(get(panels).right.w).toBe(340);
});

// In-place cell mutation keeps object identity — Svelte then never
// re-renders the changed frame. Occupant writes must produce fresh
// row/cell objects (and leave untouched slots identical).
test('occupant writes are immutable (fresh identities)', () => {
	seed(3);
	const [a, b, c] = ids();
	applyPreset([2]);
	const before = get(grid);
	const cell00 = before[0].cells[0];
	const cell01 = before[0].cells[1];
	assignCell(c, 0, 0);
	const after = get(grid);
	expect(after).not.toBe(before);
	expect(after[0]).not.toBe(before[0]);
	expect(after[0].cells[0]).not.toBe(cell00);
	expect(after[0].cells[1]).toBe(cell01); // untouched slot keeps identity
	expect(after[0].cells[0].paneId).toBe(c);
});
