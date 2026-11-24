package stock

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestParseExpiry(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Date
	}{
		// The carton convention: a month and year means the last day of it.
		{"slash", "03/2027", NewDate(2027, time.March, 31)},
		{"single digit month", "3/2027", NewDate(2027, time.March, 31)},
		{"dash", "03-2027", NewDate(2027, time.March, 31)},
		{"dot", "03.2027", NewDate(2027, time.March, 31)},
		{"spaces around", "  03 / 2027  ", NewDate(2027, time.March, 31)},
		{"year first", "2027/03", NewDate(2027, time.March, 31)},
		{"short month name", "MAR 2027", NewDate(2027, time.March, 31)},
		{"lowercase month name", "mar 2027", NewDate(2027, time.March, 31)},
		{"full month name", "March 2027", NewDate(2027, time.March, 31)},
		{"sept spelling", "SEPT 2027", NewDate(2027, time.September, 30)},
		{"month name first with dash", "DEC-2026", NewDate(2026, time.December, 31)},
		{"two digit year", "03/27", NewDate(2027, time.March, 31)},
		{"two digit year month name", "nov 29", NewDate(2029, time.November, 30)},

		// Month lengths, which is the whole reason this is not string slicing.
		{"31 day month", "01/2027", NewDate(2027, time.January, 31)},
		{"30 day month", "04/2027", NewDate(2027, time.April, 30)},
		{"december", "12/2026", NewDate(2026, time.December, 31)},

		// February, every leap rule.
		{"february common year", "02/2027", NewDate(2027, time.February, 28)},
		{"february leap year", "02/2028", NewDate(2028, time.February, 29)},
		{"february century not leap", "02/2100", NewDate(2100, time.February, 28)},
		{"february 400 year leap", "02/2000", NewDate(2000, time.February, 29)},

		// An explicitly dated pack keeps its day.
		{"full iso date", "2027-03-15", NewDate(2027, time.March, 15)},
		{"full day first date", "15-03-2027", NewDate(2027, time.March, 15)},
		{"full date on a leap day", "2028-02-29", NewDate(2028, time.February, 29)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseExpiry(tc.in)
			if err != nil {
				t.Fatalf("ParseExpiry(%q) returned an error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseExpiry(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseExpiryRejects(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		empty bool
	}{
		{"blank", "", true},
		{"only spaces", "   ", true},
		{"month thirteen", "13/2027", false},
		{"month zero", "00/2027", false},
		{"not a date at all", "banana", false},
		{"three parts", "03/2027/01", false},
		{"one part", "2027", false},
		{"negative year", "03/-5", false},
		{"impossible day in full date", "2027-02-30", false},
		{"word month misspelt", "marhc 2027", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseExpiry(tc.in)
			if err == nil {
				t.Fatalf("ParseExpiry(%q) = %s, want an error", tc.in, got)
			}
			if tc.empty && !errors.Is(err, ErrEmptyDate) {
				t.Errorf("ParseExpiry(%q) error = %v, want ErrEmptyDate", tc.in, err)
			}
			if !tc.empty && errors.Is(err, ErrEmptyDate) {
				t.Errorf("ParseExpiry(%q) reported an empty date for a bad one", tc.in)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Date
		ok   bool
	}{
		{"iso", "2027-03-15", NewDate(2027, time.March, 15), true},
		{"iso slashes", "2027/03/15", NewDate(2027, time.March, 15), true},
		{"day first dash", "15-03-2027", NewDate(2027, time.March, 15), true},
		{"day first slash", "15/03/2027", NewDate(2027, time.March, 15), true},
		{"day first dot", "15.03.2027", NewDate(2027, time.March, 15), true},
		// Day-first is read day-first, never month-first.
		{"ambiguous read day first", "01-02-2027", NewDate(2027, time.February, 1), true},
		{"iso leap day", "2028-02-29", NewDate(2028, time.February, 29), true},
		{"leap day in a common year", "2027-02-29", Date{}, false},
		{"month zero", "2027-00-10", Date{}, false},
		{"day 32", "2027-01-32", Date{}, false},
		{"blank", "", Date{}, false},
		{"month and year only", "03/2027", Date{}, false},
		{"words", "next march", Date{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDate(tc.in)
			if tc.ok {
				if err != nil {
					t.Fatalf("ParseDate(%q) returned an error: %v", tc.in, err)
				}
				if got != tc.want {
					t.Errorf("ParseDate(%q) = %s, want %s", tc.in, got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseDate(%q) = %s, want an error", tc.in, got)
			}
		})
	}
}

func TestDaysInMonthAndEndOfMonth(t *testing.T) {
	cases := []struct {
		name  string
		year  int
		month time.Month
		days  int
	}{
		{"january", 2027, time.January, 31},
		{"february common", 2027, time.February, 28},
		{"february leap", 2028, time.February, 29},
		{"february 1900 not leap", 1900, time.February, 28},
		{"february 2000 leap", 2000, time.February, 29},
		{"february 2100 not leap", 2100, time.February, 28},
		{"april", 2027, time.April, 30},
		{"june", 2027, time.June, 30},
		{"december", 2027, time.December, 31},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DaysInMonth(tc.year, tc.month); got != tc.days {
				t.Errorf("DaysInMonth(%d, %s) = %d, want %d", tc.year, tc.month, got, tc.days)
			}
			want := NewDate(tc.year, tc.month, tc.days)
			if got := EndOfMonth(tc.year, tc.month); got != want {
				t.Errorf("EndOfMonth(%d, %s) = %s, want %s", tc.year, tc.month, got, want)
			}
		})
	}
}

func TestDaysBetween(t *testing.T) {
	cases := []struct {
		name string
		from Date
		to   Date
		want int
	}{
		{"same day", NewDate(2027, time.March, 15), NewDate(2027, time.March, 15), 0},
		{"next day", NewDate(2027, time.March, 15), NewDate(2027, time.March, 16), 1},
		{"backwards", NewDate(2027, time.March, 16), NewDate(2027, time.March, 15), -1},
		{"across a month end", NewDate(2027, time.March, 31), NewDate(2027, time.April, 1), 1},
		{"across a year end", NewDate(2026, time.December, 31), NewDate(2027, time.January, 1), 1},
		{"february in a common year", NewDate(2027, time.February, 28), NewDate(2027, time.March, 1), 1},
		{"february in a leap year counts the leap day", NewDate(2028, time.February, 28), NewDate(2028, time.March, 1), 2},
		{"a common year is 365 days", NewDate(2027, time.January, 1), NewDate(2028, time.January, 1), 365},
		{"a leap year is 366 days", NewDate(2028, time.January, 1), NewDate(2029, time.January, 1), 366},
		{"ninety days back from a march expiry", NewDate(2026, time.December, 31), NewDate(2027, time.March, 31), 90},
		{"a hundred and eighty days spanning a leap day", NewDate(2028, time.January, 1), NewDate(2028, time.June, 29), 180},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DaysBetween(tc.from, tc.to); got != tc.want {
				t.Errorf("DaysBetween(%s, %s) = %d, want %d", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestAddDays(t *testing.T) {
	cases := []struct {
		name string
		in   Date
		n    int
		want Date
	}{
		{"zero stays zero", Date{}, 10, Date{}},
		{"no move", NewDate(2027, time.March, 15), 0, NewDate(2027, time.March, 15)},
		{"into the next month", NewDate(2027, time.March, 31), 1, NewDate(2027, time.April, 1)},
		{"into the next year", NewDate(2026, time.December, 31), 1, NewDate(2027, time.January, 1)},
		{"back into the previous year", NewDate(2027, time.January, 1), -1, NewDate(2026, time.December, 31)},
		{"onto a leap day", NewDate(2028, time.February, 28), 1, NewDate(2028, time.February, 29)},
		{"over a leap day", NewDate(2028, time.February, 28), 2, NewDate(2028, time.March, 1)},
		{"over a common february", NewDate(2027, time.February, 28), 1, NewDate(2027, time.March, 1)},
		{"ninety days before a march month end", NewDate(2027, time.March, 31), -90, NewDate(2026, time.December, 31)},
		{"a hundred and eighty days before a june month end in a leap year", NewDate(2028, time.June, 30), -180, NewDate(2028, time.January, 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.AddDays(tc.n); got != tc.want {
				t.Errorf("%s.AddDays(%d) = %s, want %s", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

func TestAddMonthsClampsToMonthEnd(t *testing.T) {
	cases := []struct {
		name string
		in   Date
		n    int
		want Date
	}{
		{"zero stays zero", Date{}, 6, Date{}},
		{"no move", NewDate(2027, time.May, 31), 0, NewDate(2027, time.May, 31)},
		{"31 August plus six lands on a common February", NewDate(2026, time.August, 31), 6, NewDate(2027, time.February, 28)},
		{"31 August plus six lands on a leap February", NewDate(2027, time.August, 31), 6, NewDate(2028, time.February, 29)},
		{"31 January plus one", NewDate(2027, time.January, 31), 1, NewDate(2027, time.February, 28)},
		{"31 March minus one", NewDate(2027, time.March, 31), -1, NewDate(2027, time.February, 28)},
		{"31 May plus one lands on a 30 day month", NewDate(2027, time.May, 31), 1, NewDate(2027, time.June, 30)},
		{"mid month is untouched", NewDate(2027, time.January, 15), 12, NewDate(2028, time.January, 15)},
		{"back across a year boundary", NewDate(2027, time.January, 15), -1, NewDate(2026, time.December, 15)},
		{"thirteen months back", NewDate(2027, time.January, 31), -13, NewDate(2025, time.December, 31)},
		{"twenty four months on", NewDate(2027, time.February, 28), 24, NewDate(2029, time.February, 28)},
		{"leap day plus twelve months clamps", NewDate(2028, time.February, 29), 12, NewDate(2029, time.February, 28)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.AddMonths(tc.n); got != tc.want {
				t.Errorf("%s.AddMonths(%d) = %s, want %s", tc.in, tc.n, got, tc.want)
			}
		})
	}
}

func TestDateRendering(t *testing.T) {
	cases := []struct {
		name   string
		in     Date
		iso    string
		pretty string
		month  string
	}{
		{"a date", NewDate(2027, time.March, 5), "2027-03-05", "05-03-2027", "MAR 2027"},
		{"a month end", NewDate(2028, time.February, 29), "2028-02-29", "29-02-2028", "FEB 2028"},
		{"unset", Date{}, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.String(); got != tc.iso {
				t.Errorf("String() = %q, want %q", got, tc.iso)
			}
			if got := tc.in.Pretty(); got != tc.pretty {
				t.Errorf("Pretty() = %q, want %q", got, tc.pretty)
			}
			if got := tc.in.MonthLabel(); got != tc.month {
				t.Errorf("MonthLabel() = %q, want %q", got, tc.month)
			}
		})
	}
}

func TestDateJSONRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   Date
		want string
	}{
		{"a date", NewDate(2027, time.March, 15), `"2027-03-15"`},
		{"a leap day", NewDate(2028, time.February, 29), `"2028-02-29"`},
		{"unset", Date{}, `""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(raw) != tc.want {
				t.Fatalf("Marshal = %s, want %s", raw, tc.want)
			}
			var back Date
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if back != tc.in {
				t.Errorf("round trip = %s, want %s", back, tc.in)
			}
		})
	}
}

func TestDateUnmarshalAccepts(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Date
		ok   bool
	}{
		{"iso", `"2027-03-15"`, NewDate(2027, time.March, 15), true},
		{"day first", `"15-03-2027"`, NewDate(2027, time.March, 15), true},
		{"empty string", `""`, Date{}, true},
		{"blank string", `"   "`, Date{}, true},
		{"null", `null`, Date{}, true},
		{"not a string", `12345`, Date{}, false},
		{"nonsense", `"tomorrow"`, Date{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Date
			err := json.Unmarshal([]byte(tc.raw), &got)
			if tc.ok {
				if err != nil {
					t.Fatalf("Unmarshal(%s): %v", tc.raw, err)
				}
				if got != tc.want {
					t.Errorf("Unmarshal(%s) = %s, want %s", tc.raw, got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("Unmarshal(%s) = %s, want an error", tc.raw, got)
			}
		})
	}
}

func TestBeforeAfterAndZero(t *testing.T) {
	early := NewDate(2027, time.March, 1)
	late := NewDate(2027, time.March, 2)
	if !early.Before(late) || early.After(late) {
		t.Error("1 March should sort before 2 March")
	}
	if early.Before(early) || early.After(early) {
		t.Error("a date is neither before nor after itself")
	}
	if !(Date{}).IsZero() || early.IsZero() {
		t.Error("IsZero is wrong")
	}
	if got := FromTime(time.Date(2027, time.March, 15, 23, 59, 59, 0, time.UTC)); got != NewDate(2027, time.March, 15) {
		t.Errorf("FromTime dropped the wrong part: %s", got)
	}
	if today := Today(); today.IsZero() {
		t.Error("Today returned the zero date")
	}
}
