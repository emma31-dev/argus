package snapshot

import (
	"errors"
	"testing"
)

func TestGap(t *testing.T) {
	big1 := "340282366920938463463374607431768211456" // 2^128
	big2 := "340282366920938463463374607431768211457" // 2^128 + 1
	ok := []struct{ held, owed, want string }{
		{"100", "40", "60"},
		{"40", "100", "-60"},
		{"0", "0", "0"},
		{"5", "5", "0"},
		{big2, big1, "1"},
		{big1, big2, "-1"},
		{"0", big2, "-" + big2},
		{"000100", "0", "100"},
	}
	for _, tc := range ok {
		got, err := Gap(tc.held, tc.owed)
		if err != nil || got != tc.want {
			t.Errorf("Gap(%s,%s)=%q,%v want %q", tc.held, tc.owed, got, err, tc.want)
		}
	}
	for _, bad := range [][2]string{
		{"", "1"}, {"1", ""}, {"-1", "1"}, {"1", "-1"}, {"1.5", "1"}, {"1", "1.0"},
		{" 1", "1"}, {"1e3", "1"}, {"0x10", "1"}, {"+1", "1"}, {"abc", "1"}, {"1", "1_0"},
	} {
		if _, err := Gap(bad[0], bad[1]); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("Gap(%q,%q) err=%v, want ErrInvalidAmount", bad[0], bad[1], err)
		}
	}
}

func TestFormatUnits(t *testing.T) {
	tests := []struct {
		in   string
		dec  int
		want string
	}{
		{"-1500000", 6, "-1.5"},
		{"1500000", 6, "1.5"},
		{"1000000", 6, "1"},
		{"0", 6, "0"},
		{"1", 6, "0.000001"},
		{"-1", 6, "-0.000001"},
		{"123", 0, "123"},
		{"-123", 0, "-123"},
		{"1000000000000000000000000", 24, "1"},
		{"1", 24, "0.000000000000000000000001"},
		{"123456789012345678901234567890", 18, "123456789012.34567890123456789"},
		{"100", 2, "1"},
		{"10", 2, "0.1"},
		{"340282366920938463463374607431768211456", 24, "340282366920938.463463374607431768211456"},
		{"notanumber", 6, "notanumber"},
		{"5", -1, "5"},
		{"", 6, ""},
	}
	for _, tc := range tests {
		if got := FormatUnits(tc.in, tc.dec); got != tc.want {
			t.Errorf("FormatUnits(%q,%d)=%q want %q", tc.in, tc.dec, got, tc.want)
		}
	}
}

func TestParseUnits(t *testing.T) {
	ok := []struct {
		in   string
		dec  int
		want string
	}{
		{"1.5", 6, "1500000"},
		{"-1.5", 6, "-1500000"},
		{"0.000001", 6, "1"},
		{"1", 6, "1000000"},
		{".5", 1, "5"},
		{"2.", 3, "2000"},
		{"7", 0, "7"},
		{"1.000000000000000000000001", 24, "1000000000000000000000001"},
	}
	for _, tc := range ok {
		got, err := ParseUnits(tc.in, tc.dec)
		if err != nil || got != tc.want {
			t.Errorf("ParseUnits(%q,%d)=%q,%v want %q", tc.in, tc.dec, got, err, tc.want)
		}
	}
	for _, bad := range []struct {
		in  string
		dec int
	}{{"", 6}, {".", 6}, {"1.2345678", 6}, {"1.5", 0}, {"1e3", 6}, {"1.2.3", 6}, {"--1", 6}, {"a", 2}, {"1", -1}, {"+1", 2}} {
		if _, err := ParseUnits(bad.in, bad.dec); err == nil {
			t.Errorf("ParseUnits(%q,%d) should fail", bad.in, bad.dec)
		}
	}
	// Round trip.
	for _, a := range []string{"0", "1", "999999", "1000000", "123456789012345678901234567890", "-42"} {
		if got, err := ParseUnits(FormatUnits(a, 6), 6); err != nil || got != a {
			t.Errorf("round trip %q -> %q, %v", a, got, err)
		}
	}
}

func TestRatio(t *testing.T) {
	tests := []struct{ gap, owed, want string }{
		{"-5", "1000", "-0.5000"},
		{"0", "1000", "0.0000"},
		{"1000", "1000", "100.0000"},
		{"-1", "3", "-33.3333"},
		{"2", "3", "66.6667"},
		{"-2", "3", "-66.6667"}, // half away from zero on rounding
		{"1", "100000000", "0.0000"},
		{"-340282366920938463463374607431768211456", "340282366920938463463374607431768211456", "-100.0000"},
		{"5", "340282366920938463463374607431768211456", "0.0000"},
	}
	for _, tc := range tests {
		got, err := Ratio(tc.gap, tc.owed)
		if err != nil || got != tc.want {
			t.Errorf("Ratio(%s,%s)=%q,%v want %q", tc.gap, tc.owed, got, err, tc.want)
		}
	}
	if _, err := Ratio("1", "0"); !errors.Is(err, ErrZeroOwed) {
		t.Errorf("zero owed: %v", err)
	}
	for _, bad := range [][2]string{{"x", "1"}, {"1", "-1"}, {"1", "x"}, {"", "1"}} {
		if _, err := Ratio(bad[0], bad[1]); err == nil {
			t.Errorf("Ratio(%q,%q) should fail", bad[0], bad[1])
		}
	}
}
