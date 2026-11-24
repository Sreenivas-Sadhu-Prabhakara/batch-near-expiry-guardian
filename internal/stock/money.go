package stock

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Money is an amount in minor units — paise for rupees, centavos for pesos.
//
// Stock value is added up hundreds of times on one screen, so it is held as a
// whole number of minor units rather than a float. A batch worth 12.10 is 1210
// here, and a hundred of them is exactly 121000, not 121000.00000000001.
type Money int64

// MinorUnits is how many minor units make one major unit. Both markets this
// tool serves divide their unit into 100.
const MinorUnits = 100

// Currency identifies which symbol and which digit grouping to print.
type Currency string

const (
	// INR is the Indian rupee, grouped in the Indian style (1,23,456.78).
	INR Currency = "INR"
	// PHP is the Philippine peso, grouped in thousands (123,456.78).
	PHP Currency = "PHP"
)

// Symbol is the currency sign shown in front of an amount.
func (c Currency) Symbol() string {
	switch c {
	case PHP:
		return "₱"
	default:
		return "₹"
	}
}

// Valid reports whether the currency is one this tool knows.
func (c Currency) Valid() bool { return c == INR || c == PHP }

// ErrEmptyMoney lets a caller tell a blank field apart from a bad one.
var ErrEmptyMoney = errors.New("money: no amount given")

// ParseMoney reads an amount typed by a human: "1234.50", "1,234.5",
// "₹1,234.50", "1,23,456.78" and "12" are all accepted, with or without the
// currency sign and with either digit-grouping convention, because the shop's
// spreadsheet and the distributor's invoice will not agree on which to use.
//
// More than two decimal places is an error rather than a silent rounding: if a
// purchase price really carries three decimals the owner should decide how to
// round it, not this function.
func ParseMoney(s string) (Money, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, ErrEmptyMoney
	}
	cleaned := strings.NewReplacer(",", "", " ", "", " ", "", "₹", "", "₱", "", "Rs.", "", "Rs", "", "PHP", "", "INR", "").Replace(raw)
	negative := strings.HasPrefix(cleaned, "-")
	cleaned = strings.TrimPrefix(cleaned, "-")
	cleaned = strings.TrimPrefix(cleaned, "+")
	if cleaned == "" || !strings.ContainsAny(cleaned, "0123456789") {
		return 0, fmt.Errorf("money: %q is not an amount", raw)
	}

	whole, frac := cleaned, ""
	if dot := strings.IndexByte(cleaned, '.'); dot >= 0 {
		whole, frac = cleaned[:dot], cleaned[dot+1:]
	}
	if whole == "" {
		whole = "0"
	}
	if len(frac) > 2 {
		return 0, fmt.Errorf("money: %q has more than two decimal places", raw)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if !allDigits(whole) || !allDigits(frac) {
		return 0, fmt.Errorf("money: %q is not an amount", raw)
	}

	major, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: %q is too large", raw)
	}
	minor, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: %q is not an amount", raw)
	}
	total := major*MinorUnits + minor
	if negative {
		total = -total
	}
	return Money(total), nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Plain renders the amount with two decimals and no grouping and no symbol,
// which is what a CSV column and a form field want.
func (m Money) Plain() string {
	sign := ""
	n := int64(m)
	if n < 0 {
		sign, n = "-", -n
	}
	return fmt.Sprintf("%s%d.%02d", sign, n/MinorUnits, n%MinorUnits)
}

// Format renders the amount for a person to read, grouped the way the market
// groups digits and prefixed with the currency sign.
func (m Money) Format(c Currency) string {
	sign := ""
	n := int64(m)
	if n < 0 {
		sign, n = "-", -n
	}
	whole := strconv.FormatInt(n/MinorUnits, 10)
	if c == INR {
		whole = groupIndian(whole)
	} else {
		whole = groupThousands(whole)
	}
	return fmt.Sprintf("%s%s%s.%02d", sign, c.Symbol(), whole, n%MinorUnits)
}

// groupThousands inserts a comma every three digits from the right.
func groupThousands(s string) string {
	if len(s) <= 3 {
		return s
	}
	head := len(s) % 3
	if head == 0 {
		head = 3
	}
	out := s[:head]
	for i := head; i < len(s); i += 3 {
		out += "," + s[i:i+3]
	}
	return out
}

// groupIndian inserts a comma after the last three digits and then every two,
// so 12345678 reads as 1,23,45,678 — the way a rupee figure is written on an
// Indian invoice.
func groupIndian(s string) string {
	if len(s) <= 3 {
		return s
	}
	last3 := s[len(s)-3:]
	rest := s[:len(s)-3]
	var parts []string
	for len(rest) > 2 {
		parts = append([]string{rest[len(rest)-2:]}, parts...)
		rest = rest[:len(rest)-2]
	}
	if rest != "" {
		parts = append([]string{rest}, parts...)
	}
	return strings.Join(parts, ",") + "," + last3
}

// Times multiplies the amount by a whole quantity. Stock value is a unit price
// times a count of units, so this stays exact.
func Times(m Money, qty int) Money { return Money(int64(m) * int64(qty)) }

// Percent takes a percentage of the amount, rounding half away from zero at
// the minor unit — the rule a credit note is worked out with on paper, where
// half a paisa rounds up rather than disappearing.
func Percent(m Money, percent int) Money {
	if percent == 0 || m == 0 {
		return 0
	}
	n := int64(m) * int64(percent)
	negative := n < 0
	if negative {
		n = -n
	}
	out := (n + 50) / 100
	if negative {
		out = -out
	}
	return Money(out)
}
