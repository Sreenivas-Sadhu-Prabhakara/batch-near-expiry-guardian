package stock

import (
	"errors"
	"testing"
)

func TestParseMoney(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Money
	}{
		{"plain", "1234.50", 123450},
		{"thousands grouping", "1,234.50", 123450},
		{"indian grouping", "1,23,456.78", 12345678},
		{"rupee sign", "₹1,234.50", 123450},
		{"peso sign", "₱99.99", 9999},
		{"rs prefix", "Rs. 100", 10000},
		{"inr suffix", "100 INR", 10000},
		{"whole number", "12", 1200},
		{"one decimal", "12.5", 1250},
		{"leading zero decimal", "12.05", 1205},
		{"no whole part", ".5", 50},
		{"zero", "0", 0},
		{"zero with decimals", "0.00", 0},
		{"padded", "  250.00  ", 25000},
		{"negative", "-5.25", -525},
		{"explicit plus", "+5.25", 525},
		{"a paisa", "0.01", 1},
		{"large", "9,99,999.99", 99999999},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseMoney(tc.in)
			if err != nil {
				t.Fatalf("ParseMoney(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseMoney(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseMoneyRejects(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		empty bool
	}{
		{"blank", "", true},
		{"only spaces", "   ", true},
		{"letters", "abc", false},
		{"three decimals", "1.234", false},
		{"two dots", "1.2.3", false},
		{"double minus", "--5", false},
		{"sign only", "-", false},
		{"trailing letters", "100x", false},
		{"only a dot", ".", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseMoney(tc.in)
			if err == nil {
				t.Fatalf("ParseMoney(%q) = %d, want an error", tc.in, got)
			}
			if tc.empty != errors.Is(err, ErrEmptyMoney) {
				t.Errorf("ParseMoney(%q) error = %v, ErrEmptyMoney expected: %v", tc.in, err, tc.empty)
			}
		})
	}
}

func TestMoneyFormat(t *testing.T) {
	cases := []struct {
		name string
		in   Money
		inr  string
		php  string
	}{
		{"zero", 0, "₹0.00", "₱0.00"},
		{"one minor unit", 5, "₹0.05", "₱0.05"},
		{"under a unit", 999, "₹9.99", "₱9.99"},
		{"three digits", 99999, "₹999.99", "₱999.99"},
		{"four digits", 123450, "₹1,234.50", "₱1,234.50"},
		{"exactly a thousand", 100000, "₹1,000.00", "₱1,000.00"},
		{"six digits groups differently", 12345678, "₹1,23,456.78", "₱123,456.78"},
		{"ten lakh", 100000000, "₹10,00,000.00", "₱1,000,000.00"},
		{"a crore", 1000000000, "₹1,00,00,000.00", "₱10,000,000.00"},
		{"negative", -525, "-₹5.25", "-₱5.25"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Format(INR); got != tc.inr {
				t.Errorf("%d.Format(INR) = %q, want %q", tc.in, got, tc.inr)
			}
			if got := tc.in.Format(PHP); got != tc.php {
				t.Errorf("%d.Format(PHP) = %q, want %q", tc.in, got, tc.php)
			}
		})
	}
}

func TestMoneyPlainRoundTrips(t *testing.T) {
	cases := []Money{0, 1, 5, 99, 100, 999, 123450, 12345678, -525}
	for _, in := range cases {
		plain := in.Plain()
		back, err := ParseMoney(plain)
		if err != nil {
			t.Fatalf("ParseMoney(%q): %v", plain, err)
		}
		if back != in {
			t.Errorf("%d -> %q -> %d", in, plain, back)
		}
	}
	if got := Money(5).Plain(); got != "0.05" {
		t.Errorf("Plain() = %q, want 0.05", got)
	}
	if got := Money(-525).Plain(); got != "-5.25" {
		t.Errorf("Plain() = %q, want -5.25", got)
	}
}

func TestTimesIsExact(t *testing.T) {
	cases := []struct {
		name string
		unit Money
		qty  int
		want Money
	}{
		{"nothing on hand", 1210, 0, 0},
		{"one unit", 1210, 1, 1210},
		{"a strip of ten", 1210, 10, 12100},
		{"a case", 1210, 240, 290400},
		{"free goods", 0, 500, 0},
		{"a price that floats badly", 10, 3, 30},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Times(tc.unit, tc.qty); got != tc.want {
				t.Errorf("Times(%d, %d) = %d, want %d", tc.unit, tc.qty, got, tc.want)
			}
		})
	}

	// The reason money is held in minor units: a thousand additions of a price
	// that cannot be written exactly in binary still adds up to the paisa.
	var total Money
	for i := 0; i < 1000; i++ {
		total += Times(Money(1010), 7) // 10.10 x 7 = 70.70
	}
	if want := Money(7070 * 1000); total != want {
		t.Errorf("a thousand additions drifted: got %d, want %d", total, want)
	}
}

func TestPercentRoundsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		name    string
		in      Money
		percent int
		want    Money
	}{
		{"full credit", 10000, 100, 10000},
		{"half", 10000, 50, 5000},
		{"zero percent", 10000, 0, 0},
		{"zero amount", 0, 90, 0},
		{"exactly half a minor unit rounds up", 333, 50, 167},
		{"just under half rounds down", 1, 49, 0},
		{"half of one minor unit rounds up", 1, 50, 1},
		{"ninety percent", 100, 90, 90},
		{"eighty five percent of an odd amount", 12345, 85, 10493},
		{"negative rounds away from zero", -333, 50, -167},
		{"ninety two percent of a real invoice", 487500, 92, 448500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Percent(tc.in, tc.percent); got != tc.want {
				t.Errorf("Percent(%d, %d) = %d, want %d", tc.in, tc.percent, got, tc.want)
			}
		})
	}
}

func TestCurrency(t *testing.T) {
	cases := []struct {
		currency Currency
		symbol   string
		valid    bool
	}{
		{INR, "₹", true},
		{PHP, "₱", true},
		{Currency("USD"), "₹", false},
		{Currency(""), "₹", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.currency), func(t *testing.T) {
			if got := tc.currency.Symbol(); got != tc.symbol {
				t.Errorf("Symbol() = %q, want %q", got, tc.symbol)
			}
			if got := tc.currency.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
	if MinorUnits != 100 {
		t.Errorf("MinorUnits = %d; both markets divide their unit into 100", MinorUnits)
	}
}
