/**
 * Decimals-aware formatting for Argus.
 *
 * Every amount the API returns is a base-10 integer string in the token's
 * smallest unit (wNEAR has 24 decimals, so 1 wNEAR = 10^24). These values
 * routinely exceed Number.MAX_SAFE_INTEGER, so all arithmetic here is done
 * with BigInt. Numbers are only produced by `toTokenNumber`, for chart
 * y-positions, after scaling to whole tokens.
 */

/** U+2212 MINUS SIGN: typographically correct and the same width as "+". */
export const MINUS = '−';

const INT_RE = /^-?\d+$/;

/** Parse an API amount string. Returns null for missing or malformed input. */
export function parseAmount(value: string | null | undefined): bigint | null {
	if (value === null || value === undefined) return null;
	const s = String(value).trim();
	if (!INT_RE.test(s)) return null;
	return BigInt(s);
}

const POW10_CACHE: bigint[] = [];
/** 10^n as a bigint (n >= 0). */
export function pow10(n: number): bigint {
	if (n < 0 || !Number.isInteger(n)) throw new RangeError(`pow10: invalid exponent ${n}`);
	return (POW10_CACHE[n] ??= 10n ** BigInt(n));
}

export function abs(v: bigint): bigint {
	return v < 0n ? -v : v;
}

/** Insert thousands separators into a string of digits. */
export function groupDigits(digits: string, sep = ','): string {
	return digits.replace(/\B(?=(\d{3})+(?!\d))/g, sep);
}

/** Group an integer (number, bigint or integer string): 171234567 -> "171,234,567". */
export function formatInteger(value: number | bigint | string | null | undefined): string {
	if (value === null || value === undefined || value === '') return '—';
	let s: string;
	if (typeof value === 'number') {
		if (!Number.isFinite(value)) return '—';
		s = Math.trunc(value).toString();
	} else s = String(value);
	const neg = s.startsWith('-');
	const digits = neg ? s.slice(1) : s;
	if (!/^\d+$/.test(digits)) return '—';
	return (neg ? MINUS : '') + groupDigits(digits);
}

/**
 * Round |v| / 10^decimals to `fractionDigits` places (half away from zero)
 * and return the integer part and the zero-padded fraction digits.
 */
function roundUnits(v: bigint, decimals: number, fractionDigits: number) {
	const a = abs(v);
	const fd = Math.max(0, Math.min(fractionDigits, decimals));
	const drop = decimals - fd;
	let scaled = a;
	if (drop > 0) {
		const div = pow10(drop);
		scaled = a / div;
		const rem = a % div;
		if (rem * 2n >= div) scaled += 1n;
	}
	const unit = pow10(fd);
	const int = scaled / unit;
	const frac = fd > 0 ? (scaled % unit).toString().padStart(fd, '0') : '';
	return { int, frac, isZero: scaled === 0n };
}

export interface FixedOptions {
	/** Use thousands separators in the integer part (default true). */
	group?: boolean;
	/** Drop trailing zeros from the fraction, keeping at least `minFraction` digits. */
	trim?: boolean;
	minFraction?: number;
	/** 'auto': minus only; 'always': "+" for positives too; 'never': no sign. */
	sign?: 'auto' | 'always' | 'never';
	/** Use ASCII "-" instead of U+2212 (for copyable/machine-readable output). */
	asciiMinus?: boolean;
}

/**
 * Exact fixed-point rendering of a smallest-unit integer:
 * formatFixed(1234567n, 6, 2) -> "1.23".
 */
export function formatFixed(
	v: bigint,
	decimals: number,
	fractionDigits: number,
	opts: FixedOptions = {}
): string {
	const { group = true, trim = false, minFraction = 0, sign = 'auto', asciiMinus = false } = opts;
	const r = roundUnits(v, decimals, fractionDigits);
	let frac = r.frac;
	if (trim) {
		frac = frac.replace(/0+$/, '');
		if (frac.length < minFraction) frac = frac.padEnd(Math.min(minFraction, r.frac.length), '0');
	}
	const intStr = group ? groupDigits(r.int.toString()) : r.int.toString();
	const body = frac ? `${intStr}.${frac}` : intStr;
	return signPrefix(v, r.isZero, sign, asciiMinus) + body;
}

function signPrefix(v: bigint, roundedZero: boolean, sign: FixedOptions['sign'], ascii: boolean) {
	if (sign === 'never' || v === 0n || roundedZero) return '';
	if (v < 0n) return ascii ? '-' : MINUS;
	return sign === 'always' ? '+' : '';
}

/** The exact decimal value with no rounding and no grouping: "-0.000000000000000000000001". */
export function formatExact(v: bigint, decimals: number): string {
	const r = roundUnits(v, decimals, decimals);
	const frac = r.frac.replace(/0+$/, '');
	return (v < 0n ? '-' : '') + r.int.toString() + (frac ? `.${frac}` : '');
}

const COMPACT_UNITS: [bigint, string][] = [
	[10n ** 12n, 'T'],
	[10n ** 9n, 'B'],
	[10n ** 6n, 'M']
];

export interface TokenFormatOptions {
	/** Abbreviate whole-token values >= 1 million: 12.35M. */
	compact?: boolean;
	/** Show "+" on positive values (for gaps). */
	sign?: boolean;
	/** Significant digits shown for values below one token (default 4). */
	smallSignificant?: number;
}

/**
 * Human formatting of a smallest-unit amount in whole tokens, with
 * precision that adapts to magnitude:
 *
 *   >= 1M (compact)   12.35M
 *   >= 1,000          12,345.68
 *   >= 1              3.1416  (4 dp, trailing zeros trimmed to 2)
 *   <  1              0.0004321 (4 significant digits)
 *
 * Rounding is exact (BigInt). The raw value is always available separately
 * for auditing; this is for display only.
 */
export function formatTokens(
	v: bigint | null | undefined,
	decimals: number,
	opts: TokenFormatOptions = {}
): string {
	if (v === null || v === undefined) return '—';
	const { compact = false, sign = false, smallSignificant = 4 } = opts;
	const s = sign ? 'always' : 'auto';
	if (v === 0n) return '0';
	const a = abs(v);
	const one = pow10(decimals);
	const whole = a / one;

	if (compact) {
		for (const [unit, suffix] of COMPACT_UNITS) {
			if (whole >= unit) {
				// Scale so the unit becomes the "1" and keep 2 decimals.
				const scaledDecimals = decimals + (unit.toString().length - 1);
				return formatFixed(v, scaledDecimals, 2, { sign: s }) + suffix;
			}
		}
	}
	if (whole >= 1000n) return formatFixed(v, decimals, Math.min(2, decimals), { sign: s });
	if (whole >= 1n)
		return formatFixed(v, decimals, Math.min(4, decimals), { sign: s, trim: true, minFraction: 2 });

	// Below one token: show `smallSignificant` significant digits.
	const fracDigits = a.toString().padStart(decimals, '0');
	const firstNonZero = fracDigits.search(/[1-9]/); // 0-based position after the point
	const digits = Math.min(decimals, firstNonZero + smallSignificant);
	return formatFixed(v, decimals, digits, { sign: s, trim: true, minFraction: 2 });
}

/** Convenience: format an API amount string. */
export function formatAmount(
	value: string | null | undefined,
	decimals: number,
	opts: TokenFormatOptions = {}
): string {
	return formatTokens(parseAmount(value), decimals, opts);
}

/**
 * Whole-token value as a JS number, for chart geometry only. Precision is
 * ~15-16 significant digits, which is far finer than a pixel.
 */
export function toTokenNumber(v: bigint, decimals: number): number {
	const neg = v < 0n;
	const a = abs(v);
	const one = pow10(decimals);
	const int = a / one;
	const rem = a % one;
	// Take up to 17 leading fraction digits; Number() cannot use more.
	const fracStr = rem.toString().padStart(decimals, '0').slice(0, 17);
	const frac = fracStr ? Number(`0.${fracStr}`) : 0;
	const n = Number(int) + frac;
	return neg ? -n : n;
}

/**
 * gap / owed as a percentage (a JS number). The division is done in BigInt
 * at 1e-28 % resolution, so even a 1-yocto gap on a 10^9-token supply keeps
 * its sign and magnitude. Returns null when owed is zero or missing.
 */
export function ratioPercent(gap: bigint | null, owed: bigint | null): number | null {
	if (gap === null || owed === null || owed === 0n) return null;
	const q = (gap * 10n ** 30n) / owed; // percent * 1e28
	return Number(q) / 1e28;
}

/**
 * Signed, magnitude-adaptive percentage: "+0.0041%", "−12.40%", "0%".
 * Values that would round to zero render as "<0.000001%" with their sign.
 */
export function formatPercent(p: number | null | undefined, opts: { sign?: boolean } = {}): string {
	if (p === null || p === undefined || !Number.isFinite(p)) return '—';
	const { sign = true } = opts;
	if (p === 0) return '0%';
	const a = Math.abs(p);
	const prefix = p < 0 ? MINUS : sign ? '+' : '';
	if (a < 1e-6) return `${prefix}<0.000001%`;
	let body: string;
	if (a >= 100) body = a.toLocaleString('en-US', { maximumFractionDigits: 1, minimumFractionDigits: 1 });
	else if (a >= 1) body = a.toFixed(2);
	else {
		// Two significant digits.
		const digits = Math.min(6, Math.max(2, 1 - Math.floor(Math.log10(a))));
		body = a.toFixed(digits).replace(/0+$/, '').replace(/\.$/, '');
	}
	return `${prefix}${body}%`;
}

/** "45s", "12m", "1h 12m", "2d 3h". */
export function formatDuration(seconds: number | null | undefined): string {
	if (seconds === null || seconds === undefined || !Number.isFinite(seconds) || seconds < 0) return '—';
	const s = Math.round(seconds);
	if (s < 60) return `${s}s`;
	const m = Math.floor(s / 60);
	if (m < 60) {
		const rs = s % 60;
		return rs && m < 10 ? `${m}m ${rs}s` : `${m}m`;
	}
	const h = Math.floor(m / 60);
	if (h < 24) {
		const rm = m % 60;
		return rm ? `${h}h ${rm}m` : `${h}h`;
	}
	const d = Math.floor(h / 24);
	const rh = h % 24;
	return rh ? `${d}d ${rh}h` : `${d}d`;
}

/** Relative time from a millisecond delta: "just now", "12s ago", "4m ago". */
export function formatAgo(deltaMs: number | null | undefined): string {
	if (deltaMs === null || deltaMs === undefined || !Number.isFinite(deltaMs)) return '—';
	const s = Math.max(0, Math.floor(deltaMs / 1000));
	if (s < 2) return 'just now';
	if (s < 60) return `${s}s ago`;
	const m = Math.floor(s / 60);
	if (m < 60) return `${m}m ago`;
	const h = Math.floor(m / 60);
	if (h < 48) return `${h}h ago`;
	return `${Math.floor(h / 24)}d ago`;
}

function pad2(n: number) {
	return n.toString().padStart(2, '0');
}

/** "2026-11-03 12:00:04 UTC". Always UTC so readers can match explorer timestamps. */
export function formatUtc(iso: string | null | undefined, opts: { seconds?: boolean } = {}): string {
	if (!iso) return '—';
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return '—';
	const { seconds = true } = opts;
	const date = `${d.getUTCFullYear()}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())}`;
	const time = `${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())}${seconds ? `:${pad2(d.getUTCSeconds())}` : ''}`;
	return `${date} ${time} UTC`;
}

/** Shorten a long identifier in the middle: "8f3c…91ab". */
export function middleEllipsis(s: string, head = 6, tail = 4): string {
	if (s.length <= head + tail + 1) return s;
	return `${s.slice(0, head)}…${s.slice(-tail)}`;
}

/**
 * Compact number for chart axes (plain JS numbers of whole tokens):
 * 0, 950, 12.5K, 3.2M, 1.05B.
 */
export function formatAxis(n: number): string {
	const a = Math.abs(n);
	const sign = n < 0 ? MINUS : '';
	const trimmed = (x: number, dp: number) => x.toFixed(dp).replace(/\.?0+$/, '');
	if (a === 0) return '0';
	if (a >= 1e12) return `${sign}${trimmed(a / 1e12, 2)}T`;
	if (a >= 1e9) return `${sign}${trimmed(a / 1e9, 2)}B`;
	if (a >= 1e6) return `${sign}${trimmed(a / 1e6, 2)}M`;
	if (a >= 1e4) return `${sign}${trimmed(a / 1e3, 1)}K`;
	if (a >= 1) return `${sign}${a.toLocaleString('en-US', { maximumFractionDigits: 2 })}`;
	const digits = Math.min(10, Math.max(2, 2 - Math.floor(Math.log10(a))));
	return `${sign}${trimmed(a, digits)}`;
}
