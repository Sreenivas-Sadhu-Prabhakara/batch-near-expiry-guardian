package stock

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultPolicyUsesTheNamedConstants(t *testing.T) {
	p := DefaultPolicy()
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"watch tier", p.WatchDays, DefaultWatchDays},
		{"warn tier", p.WarnDays, DefaultWarnDays},
		{"urgent tier", p.UrgentDays, DefaultUrgentDays},
		{"return window opens", p.ReturnOpensDays, DefaultReturnOpensDays},
		{"return window closes", p.ReturnClosesDays, DefaultReturnClosesDays},
		{"dispatch lead time", p.ReturnLeadDays, DefaultReturnLeadDays},
		{"credit rate", p.CreditPercent, DefaultCreditPercent},
		{"closing soon notice", p.ClosingSoonDays, DefaultClosingSoonDays},
		{"loss look-back", p.LossWindowDays, DefaultLossWindowDays},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want the constant %d", tc.name, tc.got, tc.want)
		}
	}
	if p.Market != India {
		t.Errorf("market = %s, want %s", p.Market, India)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("the default policy must be valid: %v", err)
	}
	// The tiers the brief asks for, in one assertion so a change is deliberate.
	if want := []int{90, 60, 30}; p.Tiers()[0] != want[0] || p.Tiers()[1] != want[1] || p.Tiers()[2] != want[2] {
		t.Errorf("Tiers() = %v, want %v", p.Tiers(), want)
	}
	// A full credit at purchase price is the only honest default.
	if DefaultCreditPercent != 100 {
		t.Errorf("DefaultCreditPercent = %d; any deduction would be invented", DefaultCreditPercent)
	}
	// No lead time assumed, because the tool cannot know the shop's logistics.
	if DefaultReturnLeadDays != 0 {
		t.Errorf("DefaultReturnLeadDays = %d, want 0", DefaultReturnLeadDays)
	}
	if FinancialYearEndMonth != time.March || FinancialYearEndDay != 31 {
		t.Errorf("the Indian financial year ends 31 March, got %d %s", FinancialYearEndDay, FinancialYearEndMonth)
	}
	if CreditNoteCutoffMonth != time.November || CreditNoteCutoffDay != 30 {
		t.Errorf("the credit-note cut-off is 30 November, got %d %s", CreditNoteCutoffDay, CreditNoteCutoffMonth)
	}
}

func TestPolicyNormalise(t *testing.T) {
	cases := []struct {
		name  string
		in    Policy
		check func(t *testing.T, p Policy)
	}{
		{
			name: "an empty policy becomes the default",
			in:   Policy{},
			check: func(t *testing.T, p Policy) {
				if p != (Policy{
					Market: India, WatchDays: DefaultWatchDays, WarnDays: DefaultWarnDays, UrgentDays: DefaultUrgentDays,
					ReturnOpensDays: DefaultReturnOpensDays, ReturnClosesDays: DefaultReturnClosesDays,
					ReturnLeadDays: DefaultReturnLeadDays, CreditPercent: DefaultCreditPercent,
					ClosingSoonDays: DefaultClosingSoonDays, LossWindowDays: DefaultLossWindowDays,
				}) {
					t.Errorf("normalised empty policy = %+v", p)
				}
			},
		},
		{
			name: "an unknown market falls back to india",
			in:   Policy{Market: "ZZ"},
			check: func(t *testing.T, p Policy) {
				if p.Market != India {
					t.Errorf("market = %s, want %s", p.Market, India)
				}
			},
		},
		{
			name: "the philippine market has no credit-note cut-off",
			in:   Policy{Market: Philippines, ApplyCreditNoteCutoff: true},
			check: func(t *testing.T, p Policy) {
				if p.ApplyCreditNoteCutoff {
					t.Error("the Indian cut-off must be switched off outside India")
				}
				if p.Market.Currency() != PHP {
					t.Errorf("currency = %s, want %s", p.Market.Currency(), PHP)
				}
			},
		},
		{
			name: "a zero tier is treated as unset, not as zero days",
			in:   Policy{Market: India, WatchDays: 0, WarnDays: 0, UrgentDays: 0},
			check: func(t *testing.T, p Policy) {
				if p.WatchDays != DefaultWatchDays || p.WarnDays != DefaultWarnDays || p.UrgentDays != DefaultUrgentDays {
					t.Errorf("tiers = %v, want the defaults", p.Tiers())
				}
			},
		},
		{
			name: "negative values are repaired",
			in:   Policy{Market: India, ReturnClosesDays: -5, ReturnLeadDays: -2, ClosingSoonDays: -1, LossWindowDays: -400, CreditPercent: -10},
			check: func(t *testing.T, p Policy) {
				if p.ReturnClosesDays != DefaultReturnClosesDays || p.ReturnLeadDays != DefaultReturnLeadDays {
					t.Errorf("window = %d/%d", p.ReturnOpensDays, p.ReturnClosesDays)
				}
				if p.ClosingSoonDays != DefaultClosingSoonDays || p.LossWindowDays != DefaultLossWindowDays {
					t.Errorf("notice preferences = %d/%d", p.ClosingSoonDays, p.LossWindowDays)
				}
				if p.CreditPercent != DefaultCreditPercent {
					t.Errorf("credit rate = %d", p.CreditPercent)
				}
			},
		},
		{
			name: "a credit rate over a hundred is repaired",
			in:   Policy{Market: India, CreditPercent: 150},
			check: func(t *testing.T, p Policy) {
				if p.CreditPercent != DefaultCreditPercent {
					t.Errorf("credit rate = %d, want %d", p.CreditPercent, DefaultCreditPercent)
				}
			},
		},
		{
			name: "a shop's own figures survive",
			in: Policy{Market: Philippines, WatchDays: 120, WarnDays: 45, UrgentDays: 7,
				ReturnOpensDays: 240, ReturnClosesDays: 60, ReturnLeadDays: 10, CreditPercent: 85,
				ClosingSoonDays: 21, LossWindowDays: 180},
			check: func(t *testing.T, p Policy) {
				if p.WatchDays != 120 || p.WarnDays != 45 || p.UrgentDays != 7 {
					t.Errorf("tiers = %v, want 120/45/7", p.Tiers())
				}
				if p.ReturnOpensDays != 240 || p.ReturnClosesDays != 60 || p.ReturnLeadDays != 10 {
					t.Errorf("window = %d/%d lead %d", p.ReturnOpensDays, p.ReturnClosesDays, p.ReturnLeadDays)
				}
				if p.CreditPercent != 85 || p.ClosingSoonDays != 21 || p.LossWindowDays != 180 {
					t.Errorf("credit %d, closing soon %d, loss window %d", p.CreditPercent, p.ClosingSoonDays, p.LossWindowDays)
				}
			},
		},
		{
			name: "a zero closing-soon notice is read as unset",
			in:   Policy{Market: India, ClosingSoonDays: 0},
			check: func(t *testing.T, p Policy) {
				if p.ClosingSoonDays != DefaultClosingSoonDays {
					t.Errorf("closing soon = %d; zero is indistinguishable from unset in JSON, so it takes the default", p.ClosingSoonDays)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.in
			p.Normalise()
			if err := p.Validate(); err != nil {
				t.Fatalf("a normalised policy must be valid, got %v", err)
			}
			tc.check(t, p)
		})
	}
}

func TestPolicyValidate(t *testing.T) {
	ok := func(f func(p *Policy)) Policy {
		p := DefaultPolicy()
		f(&p)
		return p
	}
	cases := []struct {
		name string
		in   Policy
		want string
	}{
		{"the default", DefaultPolicy(), ""},
		{"a philippine shop", ok(func(p *Policy) { p.Market = Philippines }), ""},
		{"an unknown market", ok(func(p *Policy) { p.Market = "US" }), "market must be"},
		{"a zero urgent tier", ok(func(p *Policy) { p.UrgentDays = 0 }), "at least 1 day"},
		{"a negative urgent tier", ok(func(p *Policy) { p.UrgentDays = -1 }), "at least 1 day"},
		{"warn equal to urgent", ok(func(p *Policy) { p.WarnDays = 30 }), "must be earlier than the urgent tier"},
		{"warn inside urgent", ok(func(p *Policy) { p.WarnDays = 20 }), "must be earlier than the urgent tier"},
		{"watch equal to warn", ok(func(p *Policy) { p.WatchDays = 60 }), "must be earlier than the warn tier"},
		{"watch inside warn", ok(func(p *Policy) { p.WatchDays = 45 }), "must be earlier than the warn tier"},
		{"a window that closes after expiry", ok(func(p *Policy) { p.ReturnClosesDays = -1 }), "cannot close after"},
		{"a window that closes on the expiry day", ok(func(p *Policy) { p.ReturnClosesDays = 0 }), ""},
		{"a window that opens when it closes", ok(func(p *Policy) { p.ReturnOpensDays = 90 }), "must open"},
		{"a window that opens after it closes", ok(func(p *Policy) { p.ReturnOpensDays = 60 }), "must open"},
		{"a negative lead time", ok(func(p *Policy) { p.ReturnLeadDays = -1 }), "cannot be negative"},
		{"a lead time as wide as the window", ok(func(p *Policy) { p.ReturnLeadDays = 90 }), "leaves no usable return window"},
		{"a lead time one day inside the window", ok(func(p *Policy) { p.ReturnLeadDays = 89 }), ""},
		{"a zero credit rate", ok(func(p *Policy) { p.CreditPercent = 0 }), "between 1 and 100"},
		{"a credit rate over a hundred", ok(func(p *Policy) { p.CreditPercent = 101 }), "between 1 and 100"},
		{"a one percent credit rate", ok(func(p *Policy) { p.CreditPercent = 1 }), ""},
		{"a closing-soon notice wider than the watch tier", ok(func(p *Policy) { p.ClosingSoonDays = 91 }), "closing-soon notice"},
		{"a closing-soon notice equal to the watch tier", ok(func(p *Policy) { p.ClosingSoonDays = 90 }), ""},
		{"a zero loss look-back", ok(func(p *Policy) { p.LossWindowDays = 0 }), "loss look-back"},
		{"a ten year loss look-back", ok(func(p *Policy) { p.LossWindowDays = 3650 }), ""},
		{"an eleven year loss look-back", ok(func(p *Policy) { p.LossWindowDays = 3651 }), "loss look-back"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error containing %q, got none", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestMarket(t *testing.T) {
	cases := []struct {
		market   Market
		currency Currency
		label    string
		valid    bool
	}{
		{India, INR, "India", true},
		{Philippines, PHP, "Philippines", true},
		{Market("XX"), INR, "India", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.market), func(t *testing.T) {
			if got := tc.market.Currency(); got != tc.currency {
				t.Errorf("Currency() = %s, want %s", got, tc.currency)
			}
			if got := tc.market.Label(); got != tc.label {
				t.Errorf("Label() = %q, want %q", got, tc.label)
			}
			if got := tc.market.Valid(); got != tc.valid {
				t.Errorf("Valid() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func TestAuthoritiesAreStatedProperly(t *testing.T) {
	list := Authorities()
	if len(list) < 3 {
		t.Fatalf("only %d authorities listed", len(list))
	}
	markets := map[Market]bool{}
	for i, a := range list {
		if strings.TrimSpace(a.Point) == "" {
			t.Errorf("authority %d has no point", i)
		}
		if strings.TrimSpace(a.Source) == "" {
			t.Errorf("authority %d (%.40s) names no source", i, a.Point)
		}
		if strings.TrimSpace(a.Confirm) == "" {
			t.Errorf("authority %d (%.40s) says who to confirm with", i, a.Point)
		}
		markets[a.Scope] = true
	}
	for _, want := range []Market{India, Philippines, ""} {
		if !markets[want] {
			t.Errorf("no authority with scope %q; both markets and the shared point must be covered", want)
		}
	}

	// The most important honesty in the tool: the return window is a commercial
	// term, not a regulation, and it must say so.
	var found bool
	for _, a := range list {
		if a.Scope == "" && strings.Contains(a.Point, "No law sets that window") {
			found = true
			if !strings.Contains(strings.ToLower(a.Source), "purchase agreement") {
				t.Errorf("the window's source should point at the purchase agreement, got %q", a.Source)
			}
		}
	}
	if !found {
		t.Error("no authority states that the return window is not set by law")
	}
}
