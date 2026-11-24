package stock

import (
	"fmt"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Every figure this tool uses is a named constant, and they all live here.
//
// None of them is a number any regulator sets. The alert tiers are a choice
// about how early you want to be told; the return window is a commercial term
// in your distributor's agreement. They are defaults, not facts, and each one
// is overridable in Settings. Confirm the return terms against your own
// distributor's credit-note policy and the statutory points against the
// authority named in Authorities below.
// ─────────────────────────────────────────────────────────────────────────────

// Alert tiers, in days before the batch expires.
//
// The three tiers answer three different questions. Ninety days out the batch
// can still be returned under the commonest distributor terms, so the question
// is "return it or sell it?". Sixty days out it is usually too late to return
// and the question is "can I shift it at a discount?". Thirty days out the
// question is only "will it be gone before it dies?". The brief these came
// from asks for exactly 30/60/90, which is also how a distributor's own
// short-dated stock report is cut.
const (
	// DefaultWatchDays is the first, calm tier: on the radar.
	DefaultWatchDays = 90
	// DefaultWarnDays is the second tier: discount or move it.
	DefaultWarnDays = 60
	// DefaultUrgentDays is the last tier before the stock is dead.
	DefaultUrgentDays = 30
)

// Supplier return / credit-note window, in days before the batch expires.
//
// INDICATIVE — NOT A STATUTORY FIGURE. No law in India or the Philippines
// obliges a distributor to take back saleable near-expiry stock, and no
// regulator fixes the window. These defaults are the commonest terms in the
// pharmaceutical distribution trade in both markets: saleable stock is taken
// back from roughly six months before expiry, and refused once it is inside
// the last three months, because the distributor has to get it back to the
// manufacturer with time to spare.
//
// THE ONLY AUTHORITY FOR THESE TWO NUMBERS IS YOUR OWN PURCHASE AGREEMENT.
// They differ by distributor, by manufacturer and sometimes by product line,
// and some lines are "no return" altogether. Read your credit-note policy and
// set the real figures in Settings before you trust the money columns.
const (
	// DefaultReturnOpensDays is when the distributor will start accepting the
	// batch back: this many days before expiry.
	DefaultReturnOpensDays = 180
	// DefaultReturnClosesDays is when the distributor stops accepting it: this
	// many days before expiry.
	DefaultReturnClosesDays = 90
	// DefaultReturnLeadDays is how many days you need to physically get the
	// goods to the distributor before the window shuts. It defaults to zero
	// because no honest default exists — it is your van, your courier and your
	// distributor's goods-inward desk. Set it and every deadline moves earlier.
	DefaultReturnLeadDays = 0
	// DefaultCreditPercent is the credit you expect as a percentage of what you
	// paid for the stock. It defaults to 100 — full credit at purchase price,
	// no handling deduction — because any other default would be invented.
	// Most agreements deduct something; put your real figure in Settings.
	DefaultCreditPercent = 100
)

// Notice preferences. Neither of these is a trade or statutory figure: they
// decide how loudly the interface talks, and nothing else.
const (
	// DefaultClosingSoonDays is how close a return deadline has to be before
	// the board calls it out. A fortnight is long enough to get a pull-out
	// organised and short enough that the list stays worth reading.
	DefaultClosingSoonDays = 14
	// DefaultLossWindowDays is how far back the loss log looks when it adds up
	// what expiry has cost: one year, so the figure covers a full season of
	// buying.
	DefaultLossWindowDays = 365
)

// India: the statutory last date for a credit note against a purchase.
//
// Under section 34(2) of the Central Goods and Services Tax Act, 2017, a
// credit note for a supply may not be declared later than 30 November
// following the end of the financial year in which the supply was made (or the
// date the annual return is furnished, whichever is earlier). A distributor who
// takes stock back after that date cannot adjust the tax on it, which in
// practice means no credit note — so a batch bought long ago can be inside the
// distributor's expiry window and still be past saving.
//
// INDICATIVE. The month was 30 September until it was moved to 30 November
// with effect from 1 October 2022. Confirm the current position with the CBIC
// (cbic.gov.in) or your tax adviser before relying on a date this tool prints.
// Applies to India only; this tool knows of no Philippine equivalent and says
// so rather than guessing.
const (
	// FinancialYearEndMonth and FinancialYearEndDay close the Indian financial
	// year: 31 March.
	FinancialYearEndMonth = time.March
	FinancialYearEndDay   = 31
	// CreditNoteCutoffMonth and CreditNoteCutoffDay are the last date for the
	// credit note: 30 November after that year end.
	CreditNoteCutoffMonth = time.November
	CreditNoteCutoffDay   = 30
)

// Market selects the currency, the digit grouping and which statutory points
// apply.
type Market string

const (
	// India is the Indian market: rupees, and the CGST credit-note cut-off.
	India Market = "IN"
	// Philippines is the Philippine market: pesos, no credit-note cut-off known.
	Philippines Market = "PH"
)

// Currency is the currency a market trades in.
func (m Market) Currency() Currency {
	if m == Philippines {
		return PHP
	}
	return INR
}

// Valid reports whether the market is one this tool knows.
func (m Market) Valid() bool { return m == India || m == Philippines }

// Label names the market for a person.
func (m Market) Label() string {
	if m == Philippines {
		return "Philippines"
	}
	return "India"
}

// Policy is the shop's own copy of every figure above. It is stored in the
// data file, so a shop whose distributor takes returns up to 60 days before
// expiry sees its own deadlines and not the defaults.
type Policy struct {
	Market Market `json:"market"`

	// Alert tiers in days before expiry, largest first.
	WatchDays  int `json:"watchDays"`
	WarnDays   int `json:"warnDays"`
	UrgentDays int `json:"urgentDays"`

	// The distributor's return window, in days before expiry.
	ReturnOpensDays  int `json:"returnOpensDays"`
	ReturnClosesDays int `json:"returnClosesDays"`
	ReturnLeadDays   int `json:"returnLeadDays"`
	CreditPercent    int `json:"creditPercent"`

	// Notice preferences.
	ClosingSoonDays int `json:"closingSoonDays"`
	LossWindowDays  int `json:"lossWindowDays"`

	// ApplyCreditNoteCutoff turns the Indian section 34(2) cut-off on. It is
	// meaningless outside India and Normalise switches it off there.
	ApplyCreditNoteCutoff bool `json:"applyCreditNoteCutoff"`
}

// DefaultPolicy is the policy a fresh data file starts with.
func DefaultPolicy() Policy {
	return Policy{
		Market:                India,
		WatchDays:             DefaultWatchDays,
		WarnDays:              DefaultWarnDays,
		UrgentDays:            DefaultUrgentDays,
		ReturnOpensDays:       DefaultReturnOpensDays,
		ReturnClosesDays:      DefaultReturnClosesDays,
		ReturnLeadDays:        DefaultReturnLeadDays,
		CreditPercent:         DefaultCreditPercent,
		ClosingSoonDays:       DefaultClosingSoonDays,
		LossWindowDays:        DefaultLossWindowDays,
		ApplyCreditNoteCutoff: true,
	}
}

// Normalise fills in anything missing with the default, so a data file written
// by an older version — or hand-edited and left half-finished — still loads
// into a working policy.
//
// Every day-count treats zero as "not set" rather than "zero days", because
// JSON cannot tell the two apart and each zero would fail in the dangerous
// direction: a zero tier silences its own alert, a zero closing day claims the
// distributor takes stock back right up to the expiry — which would overstate
// the money said to be recoverable — and a zero closing-soon notice never
// raises its voice at all.
func (p *Policy) Normalise() {
	if !p.Market.Valid() {
		p.Market = India
	}
	if p.WatchDays <= 0 {
		p.WatchDays = DefaultWatchDays
	}
	if p.WarnDays <= 0 {
		p.WarnDays = DefaultWarnDays
	}
	if p.UrgentDays <= 0 {
		p.UrgentDays = DefaultUrgentDays
	}
	if p.ReturnOpensDays <= 0 {
		p.ReturnOpensDays = DefaultReturnOpensDays
	}
	if p.ReturnClosesDays <= 0 {
		p.ReturnClosesDays = DefaultReturnClosesDays
	}
	if p.ReturnLeadDays < 0 {
		p.ReturnLeadDays = DefaultReturnLeadDays
	}
	if p.CreditPercent <= 0 || p.CreditPercent > 100 {
		p.CreditPercent = DefaultCreditPercent
	}
	if p.ClosingSoonDays <= 0 {
		p.ClosingSoonDays = DefaultClosingSoonDays
	}
	if p.LossWindowDays <= 0 {
		p.LossWindowDays = DefaultLossWindowDays
	}
	if p.Market != India {
		p.ApplyCreditNoteCutoff = false
	}
}

// Validate checks a policy a person just typed. The tiers have to be in
// order, and the return window has to open before it closes, or the board
// would quietly contradict itself.
func (p Policy) Validate() error {
	if !p.Market.Valid() {
		return fmt.Errorf("market must be %s or %s", India, Philippines)
	}
	if p.UrgentDays < 1 {
		return fmt.Errorf("the urgent tier must be at least 1 day before expiry")
	}
	if p.WarnDays <= p.UrgentDays {
		return fmt.Errorf("the warn tier (%d days) must be earlier than the urgent tier (%d days)", p.WarnDays, p.UrgentDays)
	}
	if p.WatchDays <= p.WarnDays {
		return fmt.Errorf("the watch tier (%d days) must be earlier than the warn tier (%d days)", p.WatchDays, p.WarnDays)
	}
	if p.ReturnClosesDays < 0 {
		return fmt.Errorf("the return window cannot close after the batch expires")
	}
	if p.ReturnOpensDays <= p.ReturnClosesDays {
		return fmt.Errorf("the return window must open (%d days before expiry) earlier than it closes (%d days before expiry)", p.ReturnOpensDays, p.ReturnClosesDays)
	}
	if p.ReturnLeadDays < 0 {
		return fmt.Errorf("the dispatch lead time cannot be negative")
	}
	if p.ReturnLeadDays >= p.ReturnOpensDays-p.ReturnClosesDays {
		return fmt.Errorf("a dispatch lead time of %d days leaves no usable return window (the window is %d days wide)", p.ReturnLeadDays, p.ReturnOpensDays-p.ReturnClosesDays)
	}
	if p.CreditPercent < 1 || p.CreditPercent > 100 {
		return fmt.Errorf("the credit rate must be between 1 and 100 percent")
	}
	if p.ClosingSoonDays < 0 || p.ClosingSoonDays > p.WatchDays {
		return fmt.Errorf("the closing-soon notice must be between 0 and %d days", p.WatchDays)
	}
	if p.LossWindowDays < 1 || p.LossWindowDays > 3650 {
		return fmt.Errorf("the loss look-back must be between 1 and 3650 days")
	}
	return nil
}

// Tiers lists the alert tiers from earliest to latest, for a UI that wants to
// print "90 / 60 / 30" without hard-coding it a second time.
func (p Policy) Tiers() []int { return []int{p.WatchDays, p.WarnDays, p.UrgentDays} }

// Authority is one statutory or contractual point this tool leans on, with the
// body to confirm it against. They are stated in one place so the interface,
// the README and the explainer page cannot drift apart.
type Authority struct {
	// Scope is the market the point applies to, or "" when it applies to both.
	Scope Market `json:"scope"`
	// Point is what the rule says.
	Point string `json:"point"`
	// Source names the instrument.
	Source string `json:"source"`
	// Confirm is who to check the current position with.
	Confirm string `json:"confirm"`
}

// Authorities returns the points behind the tool's behaviour: what is law, what
// is only trade practice, and who to ask.
func Authorities() []Authority {
	return []Authority{
		{
			Scope:   India,
			Point:   "A drug may not be sold, or stocked or exhibited for sale, after its labelled date of expiry, and a retail sale licence requires stock to be stored as labelled with expired stock kept apart from saleable stock. Expired stock is a compliance problem, not just a loss.",
			Source:  "Drugs and Cosmetics Act, 1940 and Drugs Rules, 1945 (conditions of licence in Forms 20 and 21)",
			Confirm: "CDSCO (cdsco.gov.in) and your State Drugs Control Department",
		},
		{
			Scope:   India,
			Point:   "A credit note against a purchase cannot be declared later than 30 November following the end of the financial year of the supply, so a return after that date will not carry a tax-adjusted credit note.",
			Source:  "Section 34(2), Central Goods and Services Tax Act, 2017 (the month was September until 1 October 2022)",
			Confirm: "CBIC (cbic.gov.in) or your tax adviser",
		},
		{
			Scope:   Philippines,
			Point:   "A retail drug outlet operates under a Licence to Operate from the FDA, which requires expired products to be segregated from saleable stock and not dispensed.",
			Source:  "Republic Act No. 3720 as amended by Republic Act No. 9711 (FDA Act of 2009); Republic Act No. 10918 (Philippine Pharmacy Act, 2016)",
			Confirm: "FDA Philippines (fda.gov.ph) and the Professional Regulation Commission",
		},
		{
			Point:   "The dates this tool prints for returning stock come from the return window in Settings. No law sets that window: it is a commercial term, it differs by distributor and product, and some lines cannot be returned at all.",
			Source:  "Your own purchase agreement and your distributor's credit-note policy",
			Confirm: "Your distributor, in writing",
		},
	}
}
