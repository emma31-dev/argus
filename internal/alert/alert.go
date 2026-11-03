package alert

import (
	"math/big"
	"strings"
	"time"
)

// Alert is the message placed on the queue and delivered to users. All
// amounts are base-10 integer strings in the token's smallest unit.
type Alert struct {
	Kind       Kind   `json:"kind"`
	TokenID    string `json:"token_id"`
	Symbol     string `json:"symbol"`
	Decimals   int    `json:"decimals"`
	Gap        string `json:"gap"`         // held - owed at Block (negative in deficit)
	MaxDeficit string `json:"max_deficit"` // positive
	StartBlock uint64 `json:"start_block"`
	Block      uint64 `json:"block"`
	IncidentID int64  `json:"incident_id"`
	// DurationSeconds is set for Recovered alerts (red period length).
	DurationSeconds int64     `json:"duration_seconds,omitempty"`
	DashboardURL    string    `json:"dashboard_url,omitempty"`
	At              time.Time `json:"at"`
}

// key identifies an alert for de-duplication across retries.
func (a Alert) key() string {
	var b strings.Builder
	b.WriteString(string(a.Kind))
	b.WriteByte('|')
	b.WriteString(a.TokenID)
	b.WriteByte('|')
	b.WriteString(big.NewInt(a.IncidentID).String())
	b.WriteByte('|')
	b.WriteString(new(big.Int).SetUint64(a.Block).String())
	return b.String()
}

// FormatUnits renders a base-10 integer string in smallest units as a decimal
// number with the given number of decimals, trimming trailing zeros
// ("1500000" with 6 decimals -> "1.5"). Invalid input is returned unchanged.
func FormatUnits(amount string, decimals int) string {
	v, ok := new(big.Int).SetString(amount, 10)
	if !ok {
		return amount
	}
	neg := v.Sign() < 0
	s := new(big.Int).Abs(v).String()
	if decimals > 0 {
		if len(s) <= decimals {
			s = strings.Repeat("0", decimals-len(s)+1) + s
		}
		whole, frac := s[:len(s)-decimals], strings.TrimRight(s[len(s)-decimals:], "0")
		s = whole
		if frac != "" {
			s += "." + frac
		}
	}
	if neg && s != "0" {
		s = "-" + s
	}
	return s
}

// negate returns the decimal string of -x (used for deficit from gap).
func negate(x string) string {
	v, ok := new(big.Int).SetString(x, 10)
	if !ok {
		return x
	}
	return v.Neg(v).String()
}
