import { describe, expect, it } from 'vitest';
import {
	MINUS,
	formatAgo,
	formatAmount,
	formatAxis,
	formatDuration,
	formatExact,
	formatFixed,
	formatInteger,
	formatPercent,
	formatTokens,
	formatUtc,
	middleEllipsis,
	parseAmount,
	pow10,
	ratioPercent,
	toTokenNumber
} from './format';

const YOCTO = 24;

describe('parseAmount', () => {
	it('parses huge integer strings exactly', () => {
		expect(parseAmount('123456789000000000000000000000')).toBe(123456789000000000000000000000n);
		expect(parseAmount('-500000000000000000000')).toBe(-500000000000000000000n);
		expect(parseAmount(' 42 ')).toBe(42n);
	});
	it('rejects non-integers', () => {
		expect(parseAmount('1.5')).toBeNull();
		expect(parseAmount('1e18')).toBeNull();
		expect(parseAmount('')).toBeNull();
		expect(parseAmount(null)).toBeNull();
		expect(parseAmount(undefined)).toBeNull();
		expect(parseAmount('abc')).toBeNull();
	});
});

describe('pow10', () => {
	it('computes exact powers', () => {
		expect(pow10(0)).toBe(1n);
		expect(pow10(24)).toBe(1000000000000000000000000n);
	});
	it('rejects negative exponents', () => {
		expect(() => pow10(-1)).toThrow();
	});
});

describe('formatInteger', () => {
	it('groups block heights', () => {
		expect(formatInteger(171234567)).toBe('171,234,567');
		expect(formatInteger('171234567')).toBe('171,234,567');
		expect(formatInteger(999)).toBe('999');
		expect(formatInteger(0)).toBe('0');
		expect(formatInteger(-1234n)).toBe(`${MINUS}1,234`);
	});
	it('handles missing values', () => {
		expect(formatInteger(null)).toBe('—');
		expect(formatInteger(Number.NaN)).toBe('—');
		expect(formatInteger('x')).toBe('—');
	});
});

describe('formatFixed', () => {
	it('rounds half away from zero', () => {
		expect(formatFixed(1234567n, 6, 2)).toBe('1.23');
		expect(formatFixed(1235000n, 6, 2)).toBe('1.24');
		expect(formatFixed(-1235000n, 6, 2)).toBe(`${MINUS}1.24`);
		expect(formatFixed(999999n, 6, 2)).toBe('1.00');
	});
	it('handles 24-decimal wNEAR values beyond float precision', () => {
		// 123,456,789.0000000000000000000005 wNEAR
		const v = 123456789000000000000000000000000n + 5n;
		expect(formatFixed(v, YOCTO, 2)).toBe('123,456,789.00');
		expect(formatFixed(v, YOCTO, 24, { group: false })).toBe('123456789.000000000000000000000005');
	});
	it('omits the sign when rounding to zero', () => {
		expect(formatFixed(-1n, YOCTO, 2)).toBe('0.00');
	});
	it('supports explicit plus and ascii minus', () => {
		expect(formatFixed(5000000n, 6, 2, { sign: 'always' })).toBe('+5.00');
		expect(formatFixed(-5000000n, 6, 2, { asciiMinus: true })).toBe('-5.00');
	});
	it('trims trailing zeros to a minimum', () => {
		expect(formatFixed(1500000n, 6, 4, { trim: true, minFraction: 2 })).toBe('1.50');
		expect(formatFixed(1234500n, 6, 4, { trim: true, minFraction: 2 })).toBe('1.2345');
	});
	it('works with zero decimals', () => {
		expect(formatFixed(1234n, 0, 2)).toBe('1,234');
	});
});

describe('formatExact', () => {
	it('is lossless', () => {
		expect(formatExact(-1n, YOCTO)).toBe('-0.000000000000000000000001');
		expect(formatExact(1500000n, 6)).toBe('1.5');
		expect(formatExact(2000000n, 6)).toBe('2');
		expect(formatExact(0n, 6)).toBe('0');
	});
});

describe('formatTokens', () => {
	it('adapts precision to magnitude', () => {
		expect(formatTokens(12345678901234n, 6)).toBe('12,345,678.90');
		expect(formatTokens(3141593n, 6)).toBe('3.1416');
		expect(formatTokens(3500000n, 6)).toBe('3.50');
		expect(formatTokens(4321n, 6)).toBe('0.004321');
		expect(formatTokens(43215n, 8)).toBe('0.0004322');
	});
	it('compacts large values when asked', () => {
		expect(formatTokens(12345678901234n, 6, { compact: true })).toBe('12.35M');
		expect(formatTokens(1234567890123456n, 6, { compact: true })).toBe('1.23B');
		expect(formatTokens(123456789000000000000000000000000n, YOCTO, { compact: true })).toBe('123.46M');
		expect(formatTokens(999999000000n, 6, { compact: true })).toBe('999,999.00');
	});
	it('signs gaps', () => {
		expect(formatTokens(500000000000000000000n, YOCTO, { sign: true })).toBe('+0.0005');
		expect(formatTokens(-1000000000000000000n, 18, { sign: true })).toBe(`${MINUS}1.00`);
		expect(formatTokens(0n, 18, { sign: true })).toBe('0');
	});
	it('shows the smallest unit rather than rounding to zero', () => {
		expect(formatTokens(-1n, YOCTO, { sign: true })).toBe(`${MINUS}0.000000000000000000000001`);
	});
	it('handles null', () => {
		expect(formatTokens(null, 6)).toBe('—');
		expect(formatAmount('nope', 6)).toBe('—');
		expect(formatAmount('1000000', 6)).toBe('1.00');
	});
});

describe('toTokenNumber', () => {
	it('scales to whole tokens', () => {
		expect(toTokenNumber(1500000n, 6)).toBe(1.5);
		expect(toTokenNumber(-2500000000000000000n, 18)).toBe(-2.5);
		expect(toTokenNumber(0n, YOCTO)).toBe(0);
	});
	it('keeps relative precision for huge values', () => {
		const n = toTokenNumber(123456789123456789000000000000000n, YOCTO);
		expect(n).toBeCloseTo(123456789.123456789, 6);
	});
});

describe('ratioPercent', () => {
	it('computes gap as % of owed in bigint', () => {
		expect(ratioPercent(-5n, 100n)).toBe(-5);
		expect(ratioPercent(500000000000000000000n, 123456789000000000000000000000n)).toBeCloseTo(
			4.05e-7,
			12
		);
	});
	it('returns null for zero owed', () => {
		expect(ratioPercent(5n, 0n)).toBeNull();
		expect(ratioPercent(null, 5n)).toBeNull();
	});
});

describe('formatPercent', () => {
	it('is sign-aware and magnitude-adaptive', () => {
		expect(formatPercent(0)).toBe('0%');
		expect(formatPercent(12.3456)).toBe('+12.35%');
		expect(formatPercent(-12.3456)).toBe(`${MINUS}12.35%`);
		expect(formatPercent(0.0041)).toBe('+0.0041%');
		expect(formatPercent(0.5)).toBe('+0.5%');
		expect(formatPercent(250)).toBe('+250.0%');
		expect(formatPercent(4e-9)).toBe('+<0.000001%');
		expect(formatPercent(-4e-9)).toBe(`${MINUS}<0.000001%`);
		expect(formatPercent(null)).toBe('—');
		expect(formatPercent(3, { sign: false })).toBe('3.00%');
	});
});

describe('formatDuration', () => {
	it('renders compact durations', () => {
		expect(formatDuration(45)).toBe('45s');
		expect(formatDuration(125)).toBe('2m 5s');
		expect(formatDuration(2880)).toBe('48m');
		expect(formatDuration(4320)).toBe('1h 12m');
		expect(formatDuration(3600)).toBe('1h');
		expect(formatDuration(183600)).toBe('2d 3h');
		expect(formatDuration(-1)).toBe('—');
	});
});

describe('formatAgo', () => {
	it('renders relative time', () => {
		expect(formatAgo(500)).toBe('just now');
		expect(formatAgo(12_000)).toBe('12s ago');
		expect(formatAgo(4 * 60_000)).toBe('4m ago');
		expect(formatAgo(3 * 3_600_000)).toBe('3h ago');
		expect(formatAgo(72 * 3_600_000)).toBe('3d ago');
	});
});

describe('formatUtc', () => {
	it('formats in UTC', () => {
		expect(formatUtc('2026-11-03T12:00:04Z')).toBe('2026-11-03 12:00:04 UTC');
		expect(formatUtc('2026-11-03T12:00:04Z', { seconds: false })).toBe('2026-11-03 12:00 UTC');
		expect(formatUtc('garbage')).toBe('—');
		expect(formatUtc(null)).toBe('—');
	});
});

describe('formatAxis', () => {
	it('compacts axis ticks', () => {
		expect(formatAxis(0)).toBe('0');
		expect(formatAxis(950)).toBe('950');
		expect(formatAxis(12500)).toBe('12.5K');
		expect(formatAxis(3200000)).toBe('3.2M');
		expect(formatAxis(-1050000000)).toBe(`${MINUS}1.05B`);
		expect(formatAxis(0.0025)).toBe('0.0025');
	});
});

describe('middleEllipsis', () => {
	it('shortens long ids', () => {
		expect(middleEllipsis('8f3c2b9d0a1e44c7b91ab')).toBe('8f3c2b…91ab');
		expect(middleEllipsis('short')).toBe('short');
	});
});
