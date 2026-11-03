/** Small helpers shared by the hand-built SVG charts. */

let uid = 0;
/** Unique id prefix for clip paths and gradients (stable per component instance). */
export function chartId(prefix = 'c') {
	return `${prefix}${++uid}`;
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const p2 = (n: number) => n.toString().padStart(2, '0');

/** Axis label for a UTC time, picking granularity from the visible span. */
export function formatTimeTick(d: Date, spanMs: number): string {
	if (spanMs <= 2 * 86400_000) return `${p2(d.getUTCHours())}:${p2(d.getUTCMinutes())}`;
	return `${MONTHS[d.getUTCMonth()]} ${d.getUTCDate()}`;
}

/** Evenly spaced "nice" UTC time ticks. */
export function timeTicks(from: number, to: number, approx: number): Date[] {
	const span = to - from;
	if (span <= 0) return [];
	const steps = [
		60_000, 2 * 60_000, 5 * 60_000, 10 * 60_000, 15 * 60_000, 30 * 60_000,
		3600_000, 2 * 3600_000, 3 * 3600_000, 6 * 3600_000, 12 * 3600_000,
		86400_000, 2 * 86400_000, 7 * 86400_000, 14 * 86400_000
	];
	const target = span / Math.max(1, approx);
	const step = steps.find((s) => s >= target) ?? steps[steps.length - 1];
	const out: Date[] = [];
	for (let t = Math.ceil(from / step) * step; t <= to; t += step) out.push(new Date(t));
	return out;
}

/** Index of the item whose x is nearest to `x` (xs sorted ascending). */
export function nearestIndex(xs: number[], x: number): number {
	if (xs.length === 0) return -1;
	let lo = 0;
	let hi = xs.length - 1;
	while (hi - lo > 1) {
		const mid = (lo + hi) >> 1;
		if (xs[mid] < x) lo = mid;
		else hi = mid;
	}
	return Math.abs(xs[lo] - x) <= Math.abs(xs[hi] - x) ? lo : hi;
}
