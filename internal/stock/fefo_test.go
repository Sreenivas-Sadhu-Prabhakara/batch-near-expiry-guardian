package stock

import (
	"strings"
	"testing"
	"time"
)

// lot builds a batch of one product for the picking tests.
func lot(id, batchNo string, expiry Date, qty int) Batch {
	return Batch{
		ID: id, Product: "Amoxicillin 500 mg", Pack: "strip of 10", BatchNo: batchNo,
		ExpiresOn: expiry, Qty: qty, UnitCost: Money(1000), UnitPrice: Money(1500),
		Supplier: "Metro Distributors",
	}
}

func TestFEFOOrder(t *testing.T) {
	t.Run("earliest expiry first, not earliest arrival", func(t *testing.T) {
		// The lot that arrived last carries the earlier expiry, which is the
		// case first-in-first-out gets wrong.
		old := lot("b1", "OLD", NewDate(2028, time.March, 31), 100)
		old.PurchasedOn = NewDate(2026, time.January, 1)
		recent := lot("b2", "NEW", NewDate(2027, time.June, 30), 100)
		recent.PurchasedOn = NewDate(2026, time.December, 1)

		got := FEFOOrder([]Batch{old, recent})
		if got[0].BatchNo != "NEW" {
			t.Errorf("first to pick = %s, want NEW", got[0].BatchNo)
		}
	})

	t.Run("a tie on expiry falls to the earlier purchase", func(t *testing.T) {
		expiry := NewDate(2027, time.June, 30)
		a := lot("b1", "AAA", expiry, 10)
		a.PurchasedOn = NewDate(2026, time.May, 1)
		b := lot("b2", "BBB", expiry, 10)
		b.PurchasedOn = NewDate(2026, time.February, 1)
		got := FEFOOrder([]Batch{a, b})
		if got[0].BatchNo != "BBB" {
			t.Errorf("first = %s, want BBB", got[0].BatchNo)
		}
	})

	t.Run("a tie on expiry and purchase falls to the batch number", func(t *testing.T) {
		expiry := NewDate(2027, time.June, 30)
		got := FEFOOrder([]Batch{lot("b2", "ZZZ", expiry, 10), lot("b1", "AAA", expiry, 10)})
		if got[0].BatchNo != "AAA" {
			t.Errorf("first = %s, want AAA", got[0].BatchNo)
		}
	})

	t.Run("identical rows fall to the id, so the slip never reshuffles", func(t *testing.T) {
		expiry := NewDate(2027, time.June, 30)
		got := FEFOOrder([]Batch{lot("b10", "SAME", expiry, 10), lot("b2", "SAME", expiry, 10)})
		if got[0].ID != "b2" {
			t.Errorf("first = %s, want b2 (ids compare as numbers, not text)", got[0].ID)
		}
	})

	t.Run("the input is left alone", func(t *testing.T) {
		in := []Batch{lot("b1", "LATE", NewDate(2028, time.March, 31), 10), lot("b2", "EARLY", NewDate(2027, time.March, 31), 10)}
		FEFOOrder(in)
		if in[0].BatchNo != "LATE" {
			t.Error("FEFOOrder sorted the caller's slice in place")
		}
	})

	t.Run("an empty list", func(t *testing.T) {
		if got := FEFOOrder(nil); len(got) != 0 {
			t.Errorf("got %d rows", len(got))
		}
	})
}

func TestPick(t *testing.T) {
	p := testPolicy()
	asOf := NewDate(2027, time.March, 1)

	// Three lots of one product: one dying soon, one later, one already dead.
	batches := []Batch{
		lot("b1", "DIESFIRST", NewDate(2027, time.April, 30), 40),
		lot("b2", "DIESLATER", NewDate(2027, time.December, 31), 100),
		lot("b3", "ALREADYGONE", NewDate(2027, time.February, 28), 60),
	}

	cases := []struct {
		name      string
		qty       int
		wantLines []struct {
			batchNo string
			take    int
		}
		shortfall int
		available int
	}{
		{
			name: "inside the first lot",
			qty:  25,
			wantLines: []struct {
				batchNo string
				take    int
			}{{"DIESFIRST", 25}},
			available: 140,
		},
		{
			name: "exactly the first lot",
			qty:  40,
			wantLines: []struct {
				batchNo string
				take    int
			}{{"DIESFIRST", 40}},
			available: 140,
		},
		{
			name: "spilling into the second lot",
			qty:  55,
			wantLines: []struct {
				batchNo string
				take    int
			}{{"DIESFIRST", 40}, {"DIESLATER", 15}},
			available: 140,
		},
		{
			name: "every unit on hand",
			qty:  140,
			wantLines: []struct {
				batchNo string
				take    int
			}{{"DIESFIRST", 40}, {"DIESLATER", 100}},
			available: 140,
		},
		{
			name: "more than there is",
			qty:  200,
			wantLines: []struct {
				batchNo string
				take    int
			}{{"DIESFIRST", 40}, {"DIESLATER", 100}},
			shortfall: 60,
			available: 140,
		},
		{
			name: "a single unit",
			qty:  1,
			wantLines: []struct {
				batchNo string
				take    int
			}{{"DIESFIRST", 1}},
			available: 140,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pl, err := Pick(batches, "Amoxicillin 500 mg", tc.qty, asOf, p)
			if err != nil {
				t.Fatalf("Pick: %v", err)
			}
			if len(pl.Lines) != len(tc.wantLines) {
				t.Fatalf("got %d lines, want %d: %+v", len(pl.Lines), len(tc.wantLines), pl.Lines)
			}
			allocated := 0
			for i, want := range tc.wantLines {
				if pl.Lines[i].BatchNo != want.batchNo || pl.Lines[i].Take != want.take {
					t.Errorf("line %d = %s x%d, want %s x%d", i, pl.Lines[i].BatchNo, pl.Lines[i].Take, want.batchNo, want.take)
				}
				allocated += want.take
			}
			if pl.Allocated != allocated {
				t.Errorf("Allocated = %d, want %d", pl.Allocated, allocated)
			}
			if pl.Shortfall != tc.shortfall {
				t.Errorf("Shortfall = %d, want %d", pl.Shortfall, tc.shortfall)
			}
			if pl.Available != tc.available {
				t.Errorf("Available = %d, want %d — expired units are not available", pl.Available, tc.available)
			}
			if pl.Complete() != (tc.shortfall == 0) {
				t.Errorf("Complete() = %v", pl.Complete())
			}
			// The expired lot is never allocated and is always reported.
			for _, line := range pl.Lines {
				if line.BatchNo == "ALREADYGONE" {
					t.Error("an expired batch was allocated")
				}
			}
			var found bool
			for _, s := range pl.Skipped {
				if s.BatchNo == "ALREADYGONE" {
					found = true
					if !strings.Contains(s.Reason, "may not be sold") {
						t.Errorf("skip reason = %q", s.Reason)
					}
				}
			}
			if !found {
				t.Error("the expired batch was dropped silently instead of being reported")
			}
		})
	}
}

func TestPickLineCarriesTheWarning(t *testing.T) {
	p := testPolicy()
	asOf := NewDate(2027, time.April, 20)
	batches := []Batch{lot("b1", "SHORTDATED", NewDate(2027, time.April, 30), 40)}

	pl, err := Pick(batches, "Amoxicillin 500 mg", 10, asOf, p)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	line := pl.Lines[0]
	if line.Tier != TierUrgent || line.Band != BandRed {
		t.Errorf("line tier = %s/%s, want urgent/red", line.Tier, line.Band)
	}
	if line.DaysLeft != 10 {
		t.Errorf("DaysLeft = %d, want 10", line.DaysLeft)
	}
	if line.Remaining != 30 {
		t.Errorf("Remaining = %d, want 30", line.Remaining)
	}
	if line.Value != Money(10*1000) {
		t.Errorf("Value = %d, want %d", line.Value, 10*1000)
	}
	if line.Shelf != "" || line.ExpiresOn != NewDate(2027, time.April, 30) {
		t.Errorf("line = %+v", line)
	}
}

func TestPickSkipsAndRejects(t *testing.T) {
	p := testPolicy()
	asOf := NewDate(2027, time.March, 1)

	t.Run("settled and empty lots are skipped with a reason", func(t *testing.T) {
		written := lot("b1", "WRITTENOFF", NewDate(2027, time.December, 31), 50)
		written.WrittenOff = true
		written.WrittenOffOn = NewDate(2027, time.February, 1)
		returned := lot("b2", "RETURNED", NewDate(2027, time.December, 31), 50)
		returned.ReturnedOn = NewDate(2027, time.February, 1)
		empty := lot("b3", "EMPTY", NewDate(2027, time.December, 31), 0)
		good := lot("b4", "GOOD", NewDate(2027, time.December, 31), 20)

		pl, err := Pick([]Batch{written, returned, empty, good}, "Amoxicillin 500 mg", 5, asOf, p)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if len(pl.Lines) != 1 || pl.Lines[0].BatchNo != "GOOD" {
			t.Fatalf("lines = %+v", pl.Lines)
		}
		if pl.Available != 20 {
			t.Errorf("Available = %d, want 20", pl.Available)
		}
		reasons := map[string]string{}
		for _, s := range pl.Skipped {
			reasons[s.BatchNo] = s.Reason
		}
		if !strings.Contains(reasons["WRITTENOFF"], "written off") {
			t.Errorf("WRITTENOFF reason = %q", reasons["WRITTENOFF"])
		}
		if !strings.Contains(reasons["RETURNED"], "returned to the distributor on 01-02-2027") {
			t.Errorf("RETURNED reason = %q", reasons["RETURNED"])
		}
		if !strings.Contains(reasons["EMPTY"], "nothing left") {
			t.Errorf("EMPTY reason = %q", reasons["EMPTY"])
		}
	})

	t.Run("a batch with no expiry on file is never picked", func(t *testing.T) {
		blind := lot("b1", "NOEXPIRY", Date{}, 50)
		pl, err := Pick([]Batch{blind}, "Amoxicillin 500 mg", 5, asOf, p)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if len(pl.Lines) != 0 {
			t.Error("a batch with no expiry on file was allocated")
		}
		if pl.Shortfall != 5 {
			t.Errorf("Shortfall = %d, want 5", pl.Shortfall)
		}
	})

	cases := []struct {
		name    string
		product string
		qty     int
		want    string
	}{
		{"no product named", "", 5, "choose a product"},
		{"a product not on file", "Ibuprofen 400 mg", 5, "no batches of"},
		{"zero quantity", "Amoxicillin 500 mg", 0, "at least 1"},
		{"a negative quantity", "Amoxicillin 500 mg", -3, "at least 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Pick([]Batch{lot("b1", "A", NewDate(2027, time.December, 31), 10)}, tc.product, tc.qty, asOf, p)
			if err == nil {
				t.Fatalf("want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}

	t.Run("the product name is matched the way a person reads it", func(t *testing.T) {
		pl, err := Pick([]Batch{lot("b1", "A", NewDate(2027, time.December, 31), 10)}, "  amoxicillin   500 MG ", 5, asOf, p)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if len(pl.Lines) != 1 {
			t.Error("a differently-cased product name did not match")
		}
	})

	t.Run("other products are left out", func(t *testing.T) {
		other := lot("b2", "OTHER", NewDate(2027, time.January, 1), 500)
		other.Product = "Paracetamol 650 mg"
		pl, err := Pick([]Batch{lot("b1", "A", NewDate(2027, time.December, 31), 10), other}, "Amoxicillin 500 mg", 20, asOf, p)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if pl.Available != 10 || pl.Shortfall != 10 {
			t.Errorf("available %d, shortfall %d — another product leaked in", pl.Available, pl.Shortfall)
		}
	})

	t.Run("a zero as-of date means today", func(t *testing.T) {
		pl, err := Pick([]Batch{lot("b1", "A", Today().AddDays(400), 10)}, "Amoxicillin 500 mg", 1, Date{}, p)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if pl.AsOf != Today() {
			t.Errorf("AsOf = %s, want today", pl.AsOf)
		}
	})
}

func TestApplyPick(t *testing.T) {
	p := testPolicy()
	asOf := NewDate(2027, time.March, 1)

	build := func() *Book {
		bk := NewBook()
		bk.Policy = p
		bk.Batches = []Batch{
			lot("b1", "DIESFIRST", NewDate(2027, time.April, 30), 40),
			lot("b2", "DIESLATER", NewDate(2027, time.December, 31), 100),
		}
		bk.Seq = 2
		return bk
	}

	t.Run("the whole list comes off the shelf", func(t *testing.T) {
		bk := build()
		pl, err := Pick(bk.Batches, "Amoxicillin 500 mg", 55, asOf, p)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		if err := bk.ApplyPick(pl); err != nil {
			t.Fatalf("ApplyPick: %v", err)
		}
		if got := bk.Find("b1").Qty; got != 0 {
			t.Errorf("b1 = %d, want 0", got)
		}
		if got := bk.Find("b2").Qty; got != 85 {
			t.Errorf("b2 = %d, want 85", got)
		}
	})

	t.Run("nothing is deducted when one line no longer fits", func(t *testing.T) {
		bk := build()
		pl, err := Pick(bk.Batches, "Amoxicillin 500 mg", 55, asOf, p)
		if err != nil {
			t.Fatalf("Pick: %v", err)
		}
		// Somebody sold from the second lot in another tab.
		bk.Find("b2").Qty = 10
		if err := bk.ApplyPick(pl); err == nil {
			t.Fatal("want an error")
		} else if !strings.Contains(err.Error(), "rebuild the pick list") {
			t.Errorf("error = %q", err)
		}
		if got := bk.Find("b1").Qty; got != 40 {
			t.Errorf("b1 = %d, want it untouched at 40 — the deduction must be all or nothing", got)
		}
		if got := bk.Find("b2").Qty; got != 10 {
			t.Errorf("b2 = %d, want 10", got)
		}
	})

	t.Run("a batch deleted meanwhile", func(t *testing.T) {
		bk := build()
		pl, _ := Pick(bk.Batches, "Amoxicillin 500 mg", 10, asOf, p)
		if err := bk.Delete("b1"); err != nil {
			t.Fatal(err)
		}
		if err := bk.ApplyPick(pl); err == nil || !strings.Contains(err.Error(), "no longer on file") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("a batch settled meanwhile", func(t *testing.T) {
		bk := build()
		pl, _ := Pick(bk.Batches, "Amoxicillin 500 mg", 10, asOf, p)
		if err := bk.MarkReturned("b1", asOf); err != nil {
			t.Fatal(err)
		}
		if err := bk.ApplyPick(pl); err == nil || !strings.Contains(err.Error(), "returned or written off") {
			t.Errorf("error = %v", err)
		}
		if got := bk.Find("b1").Qty; got != 40 {
			t.Errorf("b1 = %d, want it untouched", got)
		}
	})

	t.Run("an empty list", func(t *testing.T) {
		bk := build()
		if err := bk.ApplyPick(PickList{}); err == nil {
			t.Error("want an error for an empty pick list")
		}
	})

	t.Run("a hand-made line with no quantity", func(t *testing.T) {
		bk := build()
		bad := PickList{Lines: []Allocation{{BatchID: "b1", BatchNo: "DIESFIRST", Take: 0}}}
		if err := bk.ApplyPick(bad); err == nil {
			t.Error("want an error")
		}
		if got := bk.Find("b1").Qty; got != 40 {
			t.Errorf("b1 = %d, want it untouched", got)
		}
	})
}
