package stock

import (
	"strings"
	"testing"
	"time"
)

// testPolicy is the default policy with the credit-note cut-off switched off,
// so the tier and window tests are not perturbed by it. The cut-off has tests
// of its own below.
func testPolicy() Policy {
	p := DefaultPolicy()
	p.ApplyCreditNoteCutoff = false
	return p
}

// TestGradeTierBoundaries walks the day counts either side of every tier edge.
// The table is written in days-left so the edges are obvious, and the asOf date
// is derived from the expiry, which means the arithmetic itself is under test
// too.
func TestGradeTierBoundaries(t *testing.T) {
	p := testPolicy()
	expiry := NewDate(2027, time.June, 30)

	cases := []struct {
		daysLeft int
		tier     Tier
		band     string
	}{
		{400, TierFresh, BandGreen},
		{92, TierFresh, BandGreen},
		{91, TierFresh, BandGreen}, // one day outside the watch tier
		{90, TierWatch, BandAmber}, // the watch edge
		{89, TierWatch, BandAmber},
		{61, TierWatch, BandAmber}, // one day outside the warn tier
		{60, TierWarn, BandAmber},  // the warn edge
		{59, TierWarn, BandAmber},
		{31, TierWarn, BandAmber}, // one day outside the urgent tier
		{30, TierUrgent, BandRed}, // the urgent edge
		{29, TierUrgent, BandRed},
		{1, TierUrgent, BandRed},
		{0, TierUrgent, BandRed}, // the labelled expiry day: still saleable
		{-1, TierExpired, BandDead},
		{-30, TierExpired, BandDead},
		{-400, TierExpired, BandDead},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(plural(tc.daysLeft, "day left", "days left"), " ", "_"), func(t *testing.T) {
			asOf := expiry.AddDays(-tc.daysLeft)
			got := Grade(expiry, Date{}, asOf, p)
			if got.Tier != tc.tier {
				t.Errorf("expiry %s as of %s: tier = %s, want %s", expiry, asOf, got.Tier, tc.tier)
			}
			if got.Band != tc.band {
				t.Errorf("expiry %s as of %s: band = %s, want %s", expiry, asOf, got.Band, tc.band)
			}
			if tc.daysLeft >= 0 {
				if got.DaysLeft != tc.daysLeft {
					t.Errorf("DaysLeft = %d, want %d", got.DaysLeft, tc.daysLeft)
				}
				if got.DaysExpired != 0 {
					t.Errorf("DaysExpired = %d, want 0", got.DaysExpired)
				}
			} else {
				if got.DaysExpired != -tc.daysLeft {
					t.Errorf("DaysExpired = %d, want %d", got.DaysExpired, -tc.daysLeft)
				}
				if got.DaysLeft != 0 {
					t.Errorf("DaysLeft = %d, want 0", got.DaysLeft)
				}
			}
			if got.Tier.Dispensable() == (tc.tier == TierExpired) {
				t.Errorf("Dispensable() = %v for tier %s", got.Tier.Dispensable(), got.Tier)
			}
		})
	}
}

// TestGradeOnRealCalendarDates pins the behaviour to actual dates, including
// month ends and both sides of a leap day, where day arithmetic goes wrong.
func TestGradeOnRealCalendarDates(t *testing.T) {
	p := testPolicy()
	cases := []struct {
		name        string
		expiry      Date
		asOf        Date
		tier        Tier
		daysLeft    int
		daysExpired int
	}{
		{"last day of a 31 day month", NewDate(2027, time.March, 31), NewDate(2027, time.March, 31), TierUrgent, 0, 0},
		{"the day after a 31 day month", NewDate(2027, time.March, 31), NewDate(2027, time.April, 1), TierExpired, 0, 1},
		{"last day of a 30 day month", NewDate(2027, time.April, 30), NewDate(2027, time.April, 30), TierUrgent, 0, 0},
		{"the day after a 30 day month", NewDate(2027, time.April, 30), NewDate(2027, time.May, 1), TierExpired, 0, 1},
		{"last day of a common february", NewDate(2027, time.February, 28), NewDate(2027, time.February, 28), TierUrgent, 0, 0},
		{"the day after a common february", NewDate(2027, time.February, 28), NewDate(2027, time.March, 1), TierExpired, 0, 1},
		{"a leap day expiry on the leap day", NewDate(2028, time.February, 29), NewDate(2028, time.February, 29), TierUrgent, 0, 0},
		{"a leap day expiry the day before", NewDate(2028, time.February, 29), NewDate(2028, time.February, 28), TierUrgent, 1, 0},
		{"a leap day expiry the day after", NewDate(2028, time.February, 29), NewDate(2028, time.March, 1), TierExpired, 0, 1},
		{"a 28 february expiry in a leap year still has 29 february after it", NewDate(2028, time.February, 28), NewDate(2028, time.February, 29), TierExpired, 0, 1},
		{"last day of the year", NewDate(2026, time.December, 31), NewDate(2026, time.December, 31), TierUrgent, 0, 0},
		{"new year's day on last year's stock", NewDate(2026, time.December, 31), NewDate(2027, time.January, 1), TierExpired, 0, 1},
		{"a watch edge that crosses a leap day", NewDate(2028, time.May, 29), NewDate(2028, time.February, 29), TierWatch, 90, 0},
		{"a day earlier than that watch edge", NewDate(2028, time.May, 29), NewDate(2028, time.February, 28), TierFresh, 91, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Grade(tc.expiry, Date{}, tc.asOf, p)
			if got.Tier != tc.tier || got.DaysLeft != tc.daysLeft || got.DaysExpired != tc.daysExpired {
				t.Errorf("Grade(%s, %s) = %s/%d left/%d expired, want %s/%d/%d",
					tc.expiry, tc.asOf, got.Tier, got.DaysLeft, got.DaysExpired, tc.tier, tc.daysLeft, tc.daysExpired)
			}
		})
	}
}

func TestGradeWithNoExpiryOnFile(t *testing.T) {
	got := Grade(Date{}, NewDate(2027, time.January, 1), NewDate(2027, time.March, 1), testPolicy())
	if got.Tier != TierUnknown {
		t.Errorf("tier = %s, want %s", got.Tier, TierUnknown)
	}
	if got.Band != BandGrey {
		t.Errorf("band = %s, want %s", got.Band, BandGrey)
	}
	if got.Tier.Dispensable() {
		t.Error("stock with no expiry on file must not be treated as dispensable")
	}
	if got.Return.State != ReturnUnknown {
		t.Errorf("return state = %s, want %s", got.Return.State, ReturnUnknown)
	}
	if got.Describe() != "No expiry date on file" {
		t.Errorf("Describe() = %q", got.Describe())
	}
}

func TestGradeHonoursOverriddenTiers(t *testing.T) {
	p := testPolicy()
	p.WatchDays, p.WarnDays, p.UrgentDays = 120, 45, 7
	if err := p.Validate(); err != nil {
		t.Fatalf("the overridden policy should be valid: %v", err)
	}
	expiry := NewDate(2027, time.June, 30)
	cases := []struct {
		daysLeft int
		tier     Tier
	}{
		{121, TierFresh},
		{120, TierWatch},
		{46, TierWatch},
		{45, TierWarn},
		{8, TierWarn},
		{7, TierUrgent},
		{0, TierUrgent},
		{-1, TierExpired},
	}
	for _, tc := range cases {
		got := Grade(expiry, Date{}, expiry.AddDays(-tc.daysLeft), p)
		if got.Tier != tc.tier {
			t.Errorf("%d days left under a 120/45/7 policy = %s, want %s", tc.daysLeft, got.Tier, tc.tier)
		}
	}
}

// TestWindowDates checks the window is measured back from the expiry on the
// real calendar, leap years included.
func TestWindowDates(t *testing.T) {
	p := testPolicy()
	cases := []struct {
		name   string
		expiry Date
		opens  Date
		closes Date
	}{
		{"a june expiry in a common year", NewDate(2027, time.June, 30), NewDate(2027, time.January, 1), NewDate(2027, time.April, 1)},
		{"the same expiry in a leap year opens a day later", NewDate(2028, time.June, 30), NewDate(2028, time.January, 2), NewDate(2028, time.April, 1)},
		{"a march month end", NewDate(2027, time.March, 31), NewDate(2026, time.October, 2), NewDate(2026, time.December, 31)},
		{"a leap february expiry", NewDate(2028, time.February, 29), NewDate(2027, time.September, 2), NewDate(2027, time.December, 1)},
		{"a common february expiry", NewDate(2027, time.February, 28), NewDate(2026, time.September, 1), NewDate(2026, time.November, 30)},
		{"a december month end", NewDate(2027, time.December, 31), NewDate(2027, time.July, 4), NewDate(2027, time.October, 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := Window(tc.expiry, Date{}, tc.opens, p)
			if w.OpensOn != tc.opens {
				t.Errorf("OpensOn = %s, want %s", w.OpensOn, tc.opens)
			}
			if w.ClosesOn != tc.closes {
				t.Errorf("ClosesOn = %s, want %s", w.ClosesOn, tc.closes)
			}
			if w.Deadline != tc.closes {
				t.Errorf("with no lead time the deadline should equal the closing date: got %s, want %s", w.Deadline, tc.closes)
			}
			if DaysBetween(w.OpensOn, tc.expiry) != p.ReturnOpensDays {
				t.Errorf("the window should open %d days before expiry", p.ReturnOpensDays)
			}
			if DaysBetween(w.ClosesOn, tc.expiry) != p.ReturnClosesDays {
				t.Errorf("the window should close %d days before expiry", p.ReturnClosesDays)
			}
		})
	}
}

// TestWindowStateBoundaries steps across both edges of the window a day at a
// time.
func TestWindowStateBoundaries(t *testing.T) {
	p := testPolicy()
	expiry := NewDate(2027, time.June, 30)
	opens := NewDate(2027, time.January, 1)
	closes := NewDate(2027, time.April, 1)

	cases := []struct {
		name     string
		asOf     Date
		state    ReturnState
		daysLeft int
	}{
		{"long before the window", NewDate(2026, time.July, 1), ReturnTooEarly, 274},
		{"the day before it opens", opens.AddDays(-1), ReturnTooEarly, 91},
		{"the day it opens", opens, ReturnOpen, 90},
		{"the day after it opens", opens.AddDays(1), ReturnOpen, 89},
		{"the day before it closes", closes.AddDays(-1), ReturnOpen, 1},
		{"the day it closes", closes, ReturnOpen, 0},
		{"the day after it closes", closes.AddDays(1), ReturnClosed, -1},
		{"well after it closes", closes.AddDays(30), ReturnClosed, -30},
		{"after the stock has died", expiry.AddDays(5), ReturnClosed, -95},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := Window(expiry, Date{}, tc.asOf, p)
			if w.State != tc.state {
				t.Errorf("as of %s: state = %s, want %s", tc.asOf, w.State, tc.state)
			}
			if w.DaysToDeadline != tc.daysLeft {
				t.Errorf("as of %s: DaysToDeadline = %d, want %d", tc.asOf, w.DaysToDeadline, tc.daysLeft)
			}
			if w.Open() != (tc.state == ReturnOpen) {
				t.Errorf("Open() = %v for state %s", w.Open(), w.State)
			}
			if w.LimitedBy != LimitByWindow {
				t.Errorf("LimitedBy = %s, want %s", w.LimitedBy, LimitByWindow)
			}
		})
	}
}

func TestWindowLeadTimePullsTheDeadlineEarlier(t *testing.T) {
	expiry := NewDate(2027, time.June, 30)
	closes := NewDate(2027, time.April, 1)

	cases := []struct {
		leadDays int
		deadline Date
	}{
		{0, closes},
		{1, NewDate(2027, time.March, 31)},
		{7, NewDate(2027, time.March, 25)},
		{32, NewDate(2027, time.February, 28)}, // back across a month end
		{89, NewDate(2027, time.January, 2)},
	}
	for _, tc := range cases {
		p := testPolicy()
		p.ReturnLeadDays = tc.leadDays
		if err := p.Validate(); err != nil {
			t.Fatalf("lead time %d should be valid: %v", tc.leadDays, err)
		}
		w := Window(expiry, Date{}, tc.deadline, p)
		if w.ClosesOn != closes {
			t.Errorf("lead time must not move the closing date: got %s", w.ClosesOn)
		}
		if w.Deadline != tc.deadline {
			t.Errorf("lead time %d: deadline = %s, want %s", tc.leadDays, w.Deadline, tc.deadline)
		}
		if w.State != ReturnOpen || w.DaysToDeadline != 0 {
			t.Errorf("lead time %d: on the deadline itself the window should still be open, got %s / %d days", tc.leadDays, w.State, w.DaysToDeadline)
		}
		if next := Window(expiry, Date{}, tc.deadline.AddDays(1), p); next.State != ReturnClosed {
			t.Errorf("lead time %d: the day after the deadline should be closed, got %s", tc.leadDays, next.State)
		}
	}
}

func TestWindowHonoursOverriddenWindow(t *testing.T) {
	p := testPolicy()
	p.ReturnOpensDays, p.ReturnClosesDays = 270, 60
	if err := p.Validate(); err != nil {
		t.Fatalf("the overridden window should be valid: %v", err)
	}
	expiry := NewDate(2027, time.June, 30)
	w := Window(expiry, Date{}, NewDate(2027, time.January, 1), p)
	if got := DaysBetween(w.OpensOn, expiry); got != 270 {
		t.Errorf("the window opened %d days before expiry, want 270", got)
	}
	if got := DaysBetween(w.ClosesOn, expiry); got != 60 {
		t.Errorf("the window closed %d days before expiry, want 60", got)
	}
	if w.OpensOn != NewDate(2026, time.October, 3) {
		t.Errorf("OpensOn = %s, want 2026-10-03", w.OpensOn)
	}
	if w.ClosesOn != NewDate(2027, time.May, 1) {
		t.Errorf("ClosesOn = %s, want 2027-05-01", w.ClosesOn)
	}
}

func TestCreditNoteCutoff(t *testing.T) {
	p := DefaultPolicy() // India, cut-off applied
	cases := []struct {
		name      string
		purchased Date
		want      Date
	}{
		{"mid year", NewDate(2026, time.June, 15), NewDate(2027, time.November, 30)},
		{"start of the financial year", NewDate(2026, time.April, 1), NewDate(2027, time.November, 30)},
		{"the day before the financial year starts", NewDate(2026, time.March, 31), NewDate(2026, time.November, 30)},
		{"february belongs to the year that is ending", NewDate(2026, time.February, 10), NewDate(2026, time.November, 30)},
		{"january belongs to the year that is ending", NewDate(2027, time.January, 1), NewDate(2027, time.November, 30)},
		{"the cut-off date itself", NewDate(2026, time.November, 30), NewDate(2027, time.November, 30)},
		{"the last day of the calendar year", NewDate(2026, time.December, 31), NewDate(2027, time.November, 30)},
		{"a leap day purchase", NewDate(2028, time.February, 29), NewDate(2028, time.November, 30)},
		{"no purchase date on file", Date{}, Date{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CreditNoteCutoff(tc.purchased, p); got != tc.want {
				t.Errorf("CreditNoteCutoff(%s) = %s, want %s", tc.purchased, got, tc.want)
			}
		})
	}

	t.Run("switched off", func(t *testing.T) {
		off := testPolicy()
		if got := CreditNoteCutoff(NewDate(2026, time.June, 15), off); !got.IsZero() {
			t.Errorf("with the cut-off switched off the result should be the zero date, got %s", got)
		}
	})
	t.Run("not applied outside india", func(t *testing.T) {
		ph := DefaultPolicy()
		ph.Market = Philippines
		ph.Normalise()
		if ph.ApplyCreditNoteCutoff {
			t.Fatal("Normalise should switch the Indian cut-off off for the Philippine market")
		}
		if got := CreditNoteCutoff(NewDate(2026, time.June, 15), ph); !got.IsZero() {
			t.Errorf("a Philippine shop should get no cut-off date, got %s", got)
		}
	})
}

func TestWindowDeadlineLimitedByCreditNoteCutoff(t *testing.T) {
	p := DefaultPolicy() // India, cut-off applied

	t.Run("the cut-off bites first", func(t *testing.T) {
		// Bought in the year to 31 March 2025, so the credit note was due by
		// 30 November 2025 — long before this batch's window closes.
		expiry := NewDate(2027, time.June, 30)
		purchased := NewDate(2024, time.May, 1)
		w := Window(expiry, purchased, NewDate(2027, time.February, 1), p)
		if w.CreditNoteCutoff != NewDate(2025, time.November, 30) {
			t.Fatalf("cut-off = %s, want 2025-11-30", w.CreditNoteCutoff)
		}
		if w.Deadline != w.CreditNoteCutoff {
			t.Errorf("deadline = %s, want the cut-off %s", w.Deadline, w.CreditNoteCutoff)
		}
		if w.LimitedBy != LimitByCreditNote {
			t.Errorf("LimitedBy = %s, want %s", w.LimitedBy, LimitByCreditNote)
		}
		if w.State != ReturnClosed {
			t.Errorf("state = %s, want %s: the credit note can no longer be raised", w.State, ReturnClosed)
		}
		if w.ClosesOn != NewDate(2027, time.April, 1) {
			t.Errorf("the distributor's own closing date should be untouched, got %s", w.ClosesOn)
		}
	})

	t.Run("the cut-off falls before the window even opens", func(t *testing.T) {
		expiry := NewDate(2030, time.June, 30)
		purchased := NewDate(2024, time.May, 1) // cut-off 30 November 2025
		w := Window(expiry, purchased, NewDate(2026, time.January, 1), p)
		if w.State != ReturnClosed {
			t.Errorf("state = %s, want %s — the window is in the future but the batch can never go back", w.State, ReturnClosed)
		}
		if w.LimitedBy != LimitByCreditNote {
			t.Errorf("LimitedBy = %s, want %s", w.LimitedBy, LimitByCreditNote)
		}
	})

	t.Run("the window bites first", func(t *testing.T) {
		expiry := NewDate(2027, time.June, 30)   // window closes 1 April 2027
		purchased := NewDate(2026, time.June, 1) // cut-off 30 November 2027
		w := Window(expiry, purchased, NewDate(2027, time.February, 1), p)
		if w.Deadline != NewDate(2027, time.April, 1) {
			t.Errorf("deadline = %s, want the window's own closing date", w.Deadline)
		}
		if w.LimitedBy != LimitByWindow {
			t.Errorf("LimitedBy = %s, want %s", w.LimitedBy, LimitByWindow)
		}
		if w.State != ReturnOpen {
			t.Errorf("state = %s, want %s", w.State, ReturnOpen)
		}
		if w.CreditNoteCutoff != NewDate(2027, time.November, 30) {
			t.Errorf("the cut-off should still be reported for information, got %s", w.CreditNoteCutoff)
		}
	})

	t.Run("the cut-off and the deadline land on the same day", func(t *testing.T) {
		// A batch whose window closes exactly on 30 November 2027.
		expiry := NewDate(2027, time.November, 30).AddDays(DefaultReturnClosesDays)
		purchased := NewDate(2026, time.June, 1) // cut-off 30 November 2027
		w := Window(expiry, purchased, NewDate(2027, time.November, 30), p)
		if w.Deadline != NewDate(2027, time.November, 30) {
			t.Fatalf("deadline = %s, want 2027-11-30", w.Deadline)
		}
		if w.LimitedBy != LimitByWindow {
			t.Errorf("a tie should be credited to the window, not the cut-off: got %s", w.LimitedBy)
		}
		if w.State != ReturnOpen {
			t.Errorf("state = %s, want %s on the deadline itself", w.State, ReturnOpen)
		}
	})
}

func TestDescribeAndAction(t *testing.T) {
	p := testPolicy()
	expiry := NewDate(2027, time.June, 30)

	cases := []struct {
		name            string
		asOf            Date
		describe        string
		actionContains  string
		supplier        string
		expectDispenses bool
	}{
		{"fresh", NewDate(2026, time.June, 1), "394 days left", "returnable from 01-01-2027", "", true},
		{"open window", NewDate(2027, time.February, 1), "149 days left", "Return to Metro Distributors by 01-04-2027", "Metro Distributors", true},
		{"open window with no supplier named", NewDate(2027, time.February, 1), "149 days left", "Return to the distributor by", "", true},
		{"window closed", NewDate(2027, time.May, 1), "60 days left", "Cannot be returned", "Metro", true},
		{"last day", expiry, "Last day today", "Cannot be returned", "Metro", true},
		{"one day left", expiry.AddDays(-1), "1 day left", "Cannot be returned", "Metro", true},
		{"expired", expiry.AddDays(1), "Expired 1 day ago", "keep apart from saleable stock", "Metro", false},
		{"long expired", expiry.AddDays(45), "Expired 45 days ago", "log for disposal", "Metro", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := Grade(expiry, Date{}, tc.asOf, p)
			if got := v.Describe(); got != tc.describe {
				t.Errorf("Describe() = %q, want %q", got, tc.describe)
			}
			if got := v.Action(tc.supplier); !strings.Contains(got, tc.actionContains) {
				t.Errorf("Action() = %q, want it to contain %q", got, tc.actionContains)
			}
			if v.Tier.Dispensable() != tc.expectDispenses {
				t.Errorf("Dispensable() = %v, want %v", v.Tier.Dispensable(), tc.expectDispenses)
			}
		})
	}

	t.Run("a credit-note limited deadline says so", func(t *testing.T) {
		// The window runs 2 November 2027 to 31 January 2028, so on
		// 15 November 2027 the batch can still go back — but the credit note
		// is due by 30 November 2027, which is the date that matters.
		india := DefaultPolicy()
		v := Grade(NewDate(2028, time.April, 30), NewDate(2026, time.June, 1), NewDate(2027, time.November, 15), india)
		if v.Return.State != ReturnOpen {
			t.Fatalf("state = %s, want %s", v.Return.State, ReturnOpen)
		}
		if v.Return.Deadline != NewDate(2027, time.November, 30) {
			t.Fatalf("deadline = %s, want the cut-off 2027-11-30", v.Return.Deadline)
		}
		if v.Return.LimitedBy != LimitByCreditNote {
			t.Fatalf("LimitedBy = %s, want %s", v.Return.LimitedBy, LimitByCreditNote)
		}
		if got := v.Action("Metro"); !strings.Contains(got, "credit-note cut-off, not the window") {
			t.Errorf("Action() = %q, want it to mention the credit-note cut-off", got)
		}
	})
	t.Run("a missed cut-off says so", func(t *testing.T) {
		india := DefaultPolicy()
		v := Grade(NewDate(2027, time.June, 30), NewDate(2024, time.May, 1), NewDate(2027, time.February, 1), india)
		if got := v.Action("Metro"); !strings.Contains(got, "Too late for a credit note") {
			t.Errorf("Action() = %q, want it to say the cut-off has passed", got)
		}
	})
	t.Run("no expiry on file", func(t *testing.T) {
		v := Grade(Date{}, Date{}, NewDate(2027, time.February, 1), p)
		if got := v.Action("Metro"); !strings.Contains(got, "record its expiry date") {
			t.Errorf("Action() = %q", got)
		}
	})
}

func TestTierRankAndLabels(t *testing.T) {
	order := []Tier{TierExpired, TierUrgent, TierWarn, TierWatch, TierFresh, TierUnknown}
	for i := 1; i < len(order); i++ {
		if order[i-1].Rank() >= order[i].Rank() {
			t.Errorf("%s should rank ahead of %s", order[i-1], order[i])
		}
	}
	for _, tier := range order {
		if tier.Label() == "" {
			t.Errorf("tier %s has no label", tier)
		}
		if tier.Band() == "" {
			t.Errorf("tier %s has no band", tier)
		}
	}
	if TierUrgent.Band() == TierExpired.Band() {
		t.Error("urgent and expired stock must not share a colour; one is saleable and the other is not")
	}
	for _, state := range []ReturnState{ReturnTooEarly, ReturnOpen, ReturnClosed, ReturnUnknown} {
		if state.Label() == "" {
			t.Errorf("return state %s has no label", state)
		}
	}
}
