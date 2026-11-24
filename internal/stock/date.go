package stock

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ISODate is the canonical date format on the wire, in the data file and in
// every CSV this tool writes.
const ISODate = "2006-01-02"

// Date is a calendar date: year, month, day, no clock and no zone.
//
// An expiry date is a calendar fact. A batch labelled EXP 03/2027 dies at the
// end of March 2027 whatever the hour, and whatever timezone the shop's laptop
// is set to. Carrying a time.Time here would invite "expired overnight" bugs
// from daylight saving and zone offsets, so the domain holds only Y/M/D and
// converts to midnight UTC — where every day is exactly 24 hours — when it has
// to count days.
type Date struct {
	Year  int        `json:"year"`
	Month time.Month `json:"month"`
	Day   int        `json:"day"`
}

// NewDate builds a date from its parts without checking them; use ParseDate or
// ParseExpiry for anything a human typed.
func NewDate(year int, month time.Month, day int) Date {
	return Date{Year: year, Month: month, Day: day}
}

// FromTime takes the calendar date of t in t's own location.
func FromTime(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// Today is the current calendar date in the machine's local zone.
func Today() Date { return FromTime(time.Now()) }

// Time renders the date as midnight UTC, so subtracting two dates gives whole
// days with no daylight-saving stumbles.
func (d Date) Time() time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// IsZero reports whether no date has been recorded.
func (d Date) IsZero() bool { return d == Date{} }

// String renders the date as YYYY-MM-DD, or "" when unset.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return d.Time().Format(ISODate)
}

// Pretty renders the date the way a shop writes it by hand, "31-03-2027".
func (d Date) Pretty() string {
	if d.IsZero() {
		return ""
	}
	return d.Time().Format("02-01-2006")
}

// MonthLabel renders the date the way it is printed on a medicine carton,
// "MAR 2027". Empty when unset.
func (d Date) MonthLabel() string {
	if d.IsZero() {
		return ""
	}
	return strings.ToUpper(d.Time().Format("Jan 2006"))
}

// MarshalJSON writes the date as a "YYYY-MM-DD" string so the data file stays
// readable in a text editor.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte(`""`), nil
	}
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON accepts "YYYY-MM-DD", "" and null.
func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*d = Date{}
		return nil
	}
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return fmt.Errorf("date: expected a quoted YYYY-MM-DD string, got %s", s)
	}
	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(inner)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// ErrEmptyDate lets a caller tell "nothing entered" apart from "entered wrongly".
var ErrEmptyDate = errors.New("date: no date given")

var dayFirstLayouts = []string{ISODate, "2006/01/02", "02-01-2006", "02/01/2006", "02.01.2006"}

// ParseDate reads a full calendar date typed by a human.
//
// YYYY-MM-DD is canonical. Day-first forms (DD-MM-YYYY, DD/MM/YYYY, DD.MM.YYYY)
// are accepted too, because that is how a purchase invoice from an Indian or
// Philippine distributor prints a date and how it comes out of the shop's
// spreadsheet. A day-first string is always read day-first: 01-02-2027 is
// 1 February 2027, never 2 January.
func ParseDate(s string) (Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Date{}, ErrEmptyDate
	}
	for _, layout := range dayFirstLayouts {
		if len(layout) != len(s) {
			continue
		}
		if t, err := time.Parse(layout, s); err == nil {
			return FromTime(t), nil
		}
	}
	return Date{}, fmt.Errorf("date: %q is not a date — use YYYY-MM-DD or DD-MM-YYYY", s)
}

// monthNames maps the short month spellings that appear on cartons.
var monthNames = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "sept": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

// ParseExpiry reads the expiry printed on a medicine pack.
//
// Most packs carry a month and a year — "EXP 03/2027", "EXP MAR 2027",
// "03-27" — and not a day. The settled trade convention, and the one every
// distributor's credit-note desk works to, is that such a pack is good through
// the LAST day of the labelled month: 03/2027 means usable to 31 March 2027,
// dead on 1 April 2027. This function applies that convention, which is why
// 02/2028 resolves to 29 February and 02/2027 to 28 February.
//
// A full date is accepted as well and taken literally, for the minority of
// packs (and for most devices, reagents and test kits) that print one.
// Two-digit years are read as 20YY, which is the only reading that makes sense
// on stock a shop is holding now.
func ParseExpiry(s string) (Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Date{}, ErrEmptyDate
	}
	// A full date wins if it parses as one, so an explicitly dated pack keeps
	// its day.
	if d, err := ParseDate(s); err == nil {
		return d, nil
	}

	cleaned := strings.ToLower(s)
	cleaned = strings.ReplaceAll(cleaned, ".", " ")
	cleaned = strings.ReplaceAll(cleaned, "/", " ")
	cleaned = strings.ReplaceAll(cleaned, "-", " ")
	fields := strings.Fields(cleaned)
	if len(fields) != 2 {
		return Date{}, fmt.Errorf("expiry: %q is not an expiry — use MM/YYYY, MMM YYYY or YYYY-MM-DD", s)
	}

	// Which field is the month is decided, not guessed. A named month names
	// itself. Otherwise a leading four-digit field is the year (2027-03) and
	// anything else is month first (03/2027, 03/27). So 11/12 is November 2012,
	// the way a carton means it, and never December 2011.
	monthField, yearField := fields[0], fields[1]
	_, firstNamed := monthNames[fields[0]]
	_, secondNamed := monthNames[fields[1]]
	switch {
	case firstNamed:
		// already the right way round
	case secondNamed, len(fields[0]) == 4 && allDigits(fields[0]):
		monthField, yearField = fields[1], fields[0]
	}
	month, year, ok := monthAndYear(monthField, yearField)
	if !ok {
		return Date{}, fmt.Errorf("expiry: %q is not an expiry — use MM/YYYY, MMM YYYY or YYYY-MM-DD", s)
	}
	return EndOfMonth(year, month), nil
}

// monthAndYear reads one field as a month and the other as a year.
//
// The year has to be written with either two digits or four. A single digit is
// a typo rather than a label — no carton says "EXP 03/5" — and reading it as
// 2005 would quietly file live stock as long expired.
func monthAndYear(monthField, yearField string) (time.Month, int, bool) {
	var month time.Month
	if named, ok := monthNames[monthField]; ok {
		month = named
	} else {
		if len(monthField) > 2 || !allDigits(monthField) {
			return 0, 0, false
		}
		n, err := strconv.Atoi(monthField)
		if err != nil || n < 1 || n > 12 {
			return 0, 0, false
		}
		month = time.Month(n)
	}
	if !allDigits(yearField) {
		return 0, 0, false
	}
	year, err := strconv.Atoi(yearField)
	if err != nil {
		return 0, 0, false
	}
	switch len(yearField) {
	case 4:
		// as written
	case 2:
		year += 2000
	default:
		return 0, 0, false
	}
	return month, year, true
}

// DaysInMonth gives the length of a month: 29 for February in a leap year.
func DaysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// EndOfMonth is the last calendar day of the given month.
func EndOfMonth(year int, month time.Month) Date {
	return Date{Year: year, Month: month, Day: DaysInMonth(year, month)}
}

// Before reports whether d falls earlier in the calendar than other.
func (d Date) Before(other Date) bool { return d.Time().Before(other.Time()) }

// After reports whether d falls later in the calendar than other.
func (d Date) After(other Date) bool { return d.Time().After(other.Time()) }

// DaysBetween counts whole calendar days from one date to the next. It is
// negative when to falls before from, and counts leap days correctly because
// both ends are midnight UTC.
func DaysBetween(from, to Date) int {
	return int(to.Time().Sub(from.Time()) / (24 * time.Hour))
}

// AddDays moves the date on by n days.
func (d Date) AddDays(n int) Date {
	if d.IsZero() {
		return d
	}
	return FromTime(d.Time().AddDate(0, 0, n))
}

// AddMonths moves the date on by n calendar months, clamping to the end of the
// target month instead of spilling into the next one.
//
// A batch received on 31 August with a six-month shelf life runs to
// 28 February, or 29 February in a leap year — not to 2 or 3 March, which is
// what naive date addition gives.
func (d Date) AddMonths(n int) Date {
	if d.IsZero() {
		return d
	}
	total := int(d.Month) - 1 + n
	year := d.Year + total/12
	month := total % 12
	if month < 0 {
		month += 12
		year--
	}
	m := time.Month(month + 1)
	day := d.Day
	if last := DaysInMonth(year, m); day > last {
		day = last
	}
	return Date{Year: year, Month: m, Day: day}
}
