package stock

import "fmt"

// Tier is how close to death one batch is on a given day.
type Tier string

const (
	// TierFresh means more than the watch tier away: nothing to do.
	TierFresh Tier = "fresh"
	// TierWatch means inside the watch tier: decide return or sell.
	TierWatch Tier = "watch"
	// TierWarn means inside the warn tier: discount or move it.
	TierWarn Tier = "warn"
	// TierUrgent means inside the urgent tier: it dies this month.
	TierUrgent Tier = "urgent"
	// TierExpired means the labelled expiry has passed. The stock may not be
	// sold or dispensed and has to be kept apart from saleable stock.
	TierExpired Tier = "expired"
	// TierUnknown means no expiry date is on file, which the domain refuses to
	// accept on a new batch but can still be read out of a hand-edited file.
	TierUnknown Tier = "unknown"
)

// Rank orders the tiers by urgency, worst first, so one sort covers the board.
func (t Tier) Rank() int {
	switch t {
	case TierExpired:
		return 0
	case TierUrgent:
		return 1
	case TierWarn:
		return 2
	case TierWatch:
		return 3
	case TierFresh:
		return 4
	default:
		return 5
	}
}

// Label names the tier for a person.
func (t Tier) Label() string {
	switch t {
	case TierExpired:
		return "Expired"
	case TierUrgent:
		return "Urgent"
	case TierWarn:
		return "Warn"
	case TierWatch:
		return "Watch"
	case TierFresh:
		return "Fresh"
	default:
		return "No expiry on file"
	}
}

// Colour bands, so the board, the returns list and the pick list all paint a
// batch the same way.
const (
	BandGreen = "green"
	BandAmber = "amber"
	BandRed   = "red"
	BandDead  = "dead"
	BandGrey  = "grey"
)

// Band is the colour for the tier.
func (t Tier) Band() string {
	switch t {
	case TierExpired:
		return BandDead
	case TierUrgent:
		return BandRed
	case TierWarn, TierWatch:
		return BandAmber
	case TierFresh:
		return BandGreen
	default:
		return BandGrey
	}
}

// Dispensable reports whether stock in this tier may still be sold. Only
// expired stock may not; short-dated stock is perfectly legal to sell, which is
// the whole point of catching it early.
func (t Tier) Dispensable() bool { return t != TierExpired && t != TierUnknown }

// ReturnState is where a batch sits relative to the distributor's window.
type ReturnState string

const (
	// ReturnTooEarly means the window has not opened: the distributor will not
	// take it back yet.
	ReturnTooEarly ReturnState = "tooEarly"
	// ReturnOpen means it can go back now.
	ReturnOpen ReturnState = "open"
	// ReturnClosed means the window has shut — including the case where it is
	// still technically open but not for long enough to get the goods there.
	ReturnClosed ReturnState = "closed"
	// ReturnUnknown means there is no expiry date to work from.
	ReturnUnknown ReturnState = "unknown"
)

// Label names the state for a person.
func (s ReturnState) Label() string {
	switch s {
	case ReturnTooEarly:
		return "Window not open yet"
	case ReturnOpen:
		return "Returnable now"
	case ReturnClosed:
		return "Window closed"
	default:
		return "No expiry on file"
	}
}

// Limiter says which deadline bites first.
const (
	// LimitByWindow means the distributor's own window closes first.
	LimitByWindow = "window"
	// LimitByCreditNote means the statutory credit-note cut-off closes first.
	LimitByCreditNote = "creditNote"
)

// ReturnWindow is when a batch can go back to the distributor.
type ReturnWindow struct {
	State ReturnState `json:"state"`
	// OpensOn is the first day the distributor will accept the batch back.
	OpensOn Date `json:"opensOn"`
	// ClosesOn is the last day the distributor will accept it.
	ClosesOn Date `json:"closesOn"`
	// Deadline is the last day you can act: ClosesOn pulled back by the
	// dispatch lead time, and pulled back again if the credit-note cut-off
	// falls earlier.
	Deadline Date `json:"deadline"`
	// LimitedBy names which rule set the deadline.
	LimitedBy string `json:"limitedBy"`
	// CreditNoteCutoff is the statutory last date for the credit note, or the
	// zero date where none applies.
	CreditNoteCutoff Date `json:"creditNoteCutoff"`
	// DaysToDeadline is whole days from the day being asked about to the
	// deadline. Zero on the deadline itself and negative once it has passed.
	DaysToDeadline int `json:"daysToDeadline"`
}

// Open reports whether the batch can go back today.
func (w ReturnWindow) Open() bool { return w.State == ReturnOpen }

// CreditNoteCutoff gives the last date a credit note can be raised against a
// purchase made on the given date, under the Indian financial-year rule in
// policy.go. It returns the zero date when the policy does not apply the
// cut-off or when the purchase date is not known.
//
// The Indian financial year runs to 31 March, and the cut-off is the
// 30 November that follows it: stock bought on 31 March 2027 is in the year
// ending that day, so its credit note is due by 30 November 2027, while stock
// bought one day later on 1 April 2027 belongs to the next year and has until
// 30 November 2028.
func CreditNoteCutoff(purchasedOn Date, p Policy) Date {
	if !p.ApplyCreditNoteCutoff || purchasedOn.IsZero() {
		return Date{}
	}
	yearEnd := purchasedOn.Year
	if purchasedOn.Month > FinancialYearEndMonth {
		yearEnd++
	}
	return Date{Year: yearEnd, Month: CreditNoteCutoffMonth, Day: CreditNoteCutoffDay}
}

// Window works out the return window for one batch.
//
// The window is measured backwards from the labelled expiry: it opens
// ReturnOpensDays before it and closes ReturnClosesDays before it. The deadline
// is earlier again by the dispatch lead time, and earlier still if the
// credit-note cut-off lands first — a batch bought three years ago can sit
// squarely inside the distributor's window and still be past saving, which is
// exactly the case a shop never spots by eye.
func Window(expiresOn, purchasedOn, asOf Date, p Policy) ReturnWindow {
	if expiresOn.IsZero() {
		return ReturnWindow{State: ReturnUnknown, LimitedBy: LimitByWindow}
	}
	opens := expiresOn.AddDays(-p.ReturnOpensDays)
	closes := expiresOn.AddDays(-p.ReturnClosesDays)
	deadline := closes.AddDays(-p.ReturnLeadDays)
	limited := LimitByWindow

	cutoff := CreditNoteCutoff(purchasedOn, p)
	if !cutoff.IsZero() && cutoff.Before(deadline) {
		deadline = cutoff
		limited = LimitByCreditNote
	}

	w := ReturnWindow{
		OpensOn:          opens,
		ClosesOn:         closes,
		Deadline:         deadline,
		LimitedBy:        limited,
		CreditNoteCutoff: cutoff,
		DaysToDeadline:   DaysBetween(asOf, deadline),
	}
	// The deadline is tested before the opening date, and deliberately. A batch
	// bought three financial years ago can have a credit-note cut-off that
	// falls before the distributor's window even opens: the window is still in
	// the future, but the batch can never go back. Reporting that as "not open
	// yet" would tell the owner to wait for a day that will not help.
	switch {
	case w.DaysToDeadline < 0:
		w.State = ReturnClosed
	case asOf.Before(opens):
		w.State = ReturnTooEarly
	default:
		w.State = ReturnOpen
	}
	return w
}

// Verdict is everything the tool knows about one batch on one day.
type Verdict struct {
	Tier Tier   `json:"tier"`
	Band string `json:"band"`
	// DaysLeft is whole days from the day being asked about to the labelled
	// expiry. It is zero on the expiry day itself and zero once expired.
	DaysLeft int `json:"daysLeft"`
	// DaysExpired is whole days since the expiry passed, at least 1 whenever
	// the tier is expired.
	DaysExpired int          `json:"daysExpired"`
	Return      ReturnWindow `json:"return"`
}

// Grade places one batch in a tier and works out its return window.
//
// A pack is good through the whole of its labelled expiry date. A carton marked
// EXP 03/2027 may be dispensed on 31 March 2027 and not on 1 April, so the
// expiry day itself is the most urgent live tier with zero days left, and the
// day after it is expired by one day. Getting this off by one would either
// condemn a day of saleable stock or let a shop dispense a dead pack.
func Grade(expiresOn, purchasedOn, asOf Date, p Policy) Verdict {
	v := Verdict{Return: Window(expiresOn, purchasedOn, asOf, p)}
	if expiresOn.IsZero() {
		v.Tier = TierUnknown
		v.Band = TierUnknown.Band()
		return v
	}
	left := DaysBetween(asOf, expiresOn)
	switch {
	case left < 0:
		v.Tier, v.DaysExpired = TierExpired, -left
	case left <= p.UrgentDays:
		v.Tier, v.DaysLeft = TierUrgent, left
	case left <= p.WarnDays:
		v.Tier, v.DaysLeft = TierWarn, left
	case left <= p.WatchDays:
		v.Tier, v.DaysLeft = TierWatch, left
	default:
		v.Tier, v.DaysLeft = TierFresh, left
	}
	v.Band = v.Tier.Band()
	return v
}

// Describe puts the expiry side of the verdict into the one line a table row
// has room for.
func (v Verdict) Describe() string {
	switch v.Tier {
	case TierExpired:
		return fmt.Sprintf("Expired %s ago", plural(v.DaysExpired, "day", "days"))
	case TierUnknown:
		return "No expiry date on file"
	default:
		if v.DaysLeft == 0 {
			return "Last day today"
		}
		return fmt.Sprintf("%s left", plural(v.DaysLeft, "day", "days"))
	}
}

// Action is the single next thing to do with this batch, which is the only
// sentence a shop owner actually reads.
func (v Verdict) Action(supplier string) string {
	if supplier == "" {
		supplier = "the distributor"
	}
	switch v.Tier {
	case TierUnknown:
		return "Find the pack and record its expiry date"
	case TierExpired:
		return "Pull from the shelf, keep apart from saleable stock, log for disposal"
	}
	switch v.Return.State {
	case ReturnOpen:
		if v.Return.LimitedBy == LimitByCreditNote {
			return fmt.Sprintf("Return to %s by %s — credit-note cut-off, not the window", supplier, v.Return.Deadline.Pretty())
		}
		return fmt.Sprintf("Return to %s by %s", supplier, v.Return.Deadline.Pretty())
	case ReturnTooEarly:
		return fmt.Sprintf("Sell normally; returnable from %s", v.Return.OpensOn.Pretty())
	default:
		if v.Return.LimitedBy == LimitByCreditNote {
			return fmt.Sprintf("Too late for a credit note (cut-off was %s) — discount it, move it, or write it off", v.Return.CreditNoteCutoff.Pretty())
		}
		if v.Tier == TierFresh {
			return "Nothing to do"
		}
		return "Cannot be returned — discount it, move it to a busier counter, or write it off"
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// MonthEndExpiry is the convention ParseExpiry applies, stated once so the
// interface and the explainer can quote it without re-wording it.
const MonthEndExpiry = "A pack labelled with only a month and year is treated as good through the last day of that month."
