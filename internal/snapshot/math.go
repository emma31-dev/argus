// Package snapshot computes owed/held/gap readings for Intents tokens.
//
// The arithmetic is pure and uses math/big on base-10 integer strings; no
// floats are involved anywhere. Reader performs one same-block snapshot
// round over a set of tokens through a small Caller interface.
package snapshot

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ErrInvalidAmount means a string is not a valid base-10 integer amount.
var ErrInvalidAmount = errors.New("snapshot: invalid amount")

// ErrZeroOwed is returned by Ratio when owed is zero.
var ErrZeroOwed = errors.New("snapshot: owed is zero")

// RatioDecimals is the number of fractional digits Ratio returns.
const RatioDecimals = 4

// parseUint parses a non-negative base-10 integer made only of ASCII digits
// (no sign, spaces, exponent or decimal point).
func parseUint(s string) (*big.Int, error) {
	if s == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidAmount)
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return nil, fmt.Errorf("%w: %q", ErrInvalidAmount, s)
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAmount, s)
	}
	return n, nil
}

// parseInt parses an optionally negative base-10 integer.
func parseInt(s string) (*big.Int, error) {
	neg := strings.HasPrefix(s, "-")
	n, err := parseUint(strings.TrimPrefix(s, "-"))
	if err != nil {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAmount, s)
	}
	if neg {
		n.Neg(n)
	}
	return n, nil
}

// Gap returns held - owed as a base-10 string. held and owed must be
// non-negative integers; the result may be negative.
func Gap(held, owed string) (string, error) {
	h, err := parseUint(held)
	if err != nil {
		return "", fmt.Errorf("held: %w", err)
	}
	o, err := parseUint(owed)
	if err != nil {
		return "", fmt.Errorf("owed: %w", err)
	}
	return new(big.Int).Sub(h, o).String(), nil
}

// FormatUnits renders an integer amount of smallest units as a decimal
// token amount, exactly: ("-1500000", 6) -> "-1.5". Trailing zeros of the
// fraction are dropped. If amount is not a valid integer or decimals is
// negative, amount is returned unchanged.
func FormatUnits(amount string, decimals int) string {
	n, err := parseInt(amount)
	if err != nil || decimals < 0 {
		return amount
	}
	sign := ""
	if n.Sign() < 0 {
		sign = "-"
		n.Abs(n)
	}
	digits := n.String()
	if decimals == 0 {
		return sign + digits
	}
	if len(digits) <= decimals {
		digits = strings.Repeat("0", decimals-len(digits)+1) + digits
	}
	whole, frac := digits[:len(digits)-decimals], strings.TrimRight(digits[len(digits)-decimals:], "0")
	if frac == "" {
		return sign + whole
	}
	return sign + whole + "." + frac
}

// ParseUnits is the inverse of FormatUnits: "1.5", 6 -> "1500000". It
// accepts an optional leading "-", rejects exponents and more fractional
// digits than decimals (no silent rounding).
func ParseUnits(s string, decimals int) (string, error) {
	if decimals < 0 {
		return "", fmt.Errorf("%w: negative decimals", ErrInvalidAmount)
	}
	neg := strings.HasPrefix(s, "-")
	body := strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(body, ".")
	if whole == "" && frac == "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidAmount, s)
	}
	if strings.Contains(frac, ".") || len(frac) > decimals {
		return "", fmt.Errorf("%w: %q has more than %d fractional digits", ErrInvalidAmount, s, decimals)
	}
	if whole == "" {
		whole = "0"
	}
	n, err := parseUint(whole + frac + strings.Repeat("0", decimals-len(frac)))
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidAmount, s)
	}
	if neg {
		n.Neg(n)
	}
	return n.String(), nil
}

// Ratio returns gap as a percentage of owed, with RatioDecimals fractional
// digits, rounded half away from zero: Ratio("-5", "1000") -> "-0.5000".
// gap may be negative; owed must be a positive integer (ErrZeroOwed
// otherwise).
func Ratio(gap, owed string) (string, error) {
	g, err := parseInt(gap)
	if err != nil {
		return "", fmt.Errorf("gap: %w", err)
	}
	o, err := parseUint(owed)
	if err != nil {
		return "", fmt.Errorf("owed: %w", err)
	}
	if o.Sign() == 0 {
		return "", ErrZeroOwed
	}
	r := new(big.Rat).SetFrac(new(big.Int).Mul(g, big.NewInt(100)), o)
	return r.FloatString(RatioDecimals), nil
}
