package stock

import (
	"testing"
	"time"
)

// boardAsOf is the day every board test is taken on. Each batch below is
// positioned relative to it so that the tier and the window state are both
// deliberate, and the money adds up to round figures that can be checked by
// hand against the table in the test.
var boardAsOf = NewDate(2027, time.March, 1)

// boardBook is six live batches and two settled ones.
//
//	id  product   expiry      days left  tier     window      cost value
//	b1  Fresh     2028-06-30  487        fresh    too early   1,000.00
//	b2  Watch     2027-05-30  90         watch    open today  1,000.00
//	b3  Warn      2027-04-30  60         warn     closed      1,000.00
//	b4  Urgent    2027-03-15  14         urgent   closed        300.00
//	b5  Dead      2027-02-28  -1         expired  closed      1,000.00
//	b6  OpenWide  2027-07-31  152        fresh    open         1,000.00
//	b7  Loss      2027-01-31  written off on 2027-02-10         600.00
//	b8  Saved     2027-09-30  returned on 2027-02-20            300.00
func boardBook(t *testing.T) *Book {
	t.Helper()
	bk := NewBook()
	bk.Policy = testPolicy()

	add := func(id, product string, expiry Date, qty int, unitCost Money) *Batch {
		b := Batch{
			ID: id, Product: product, Pack: "strip of 10", BatchNo: "B" + id,
			ExpiresOn: expiry, Qty: qty,
			UnitCost:  unitCost,
			UnitPrice: unitCost + unitCost/2, // a flat half mark-up keeps the retail column checkable
			Supplier:  "Metro Distributors", Shelf: "A1",
		}
		b.Normalise()
		if err := b.Validate(); err != nil {
			t.Fatalf("%s is not a valid batch: %v", id, err)
		}
		bk.Batches = append(bk.Batches, b)
		bk.Seq++
		return &bk.Batches[len(bk.Batches)-1]
	}

	add("b1", "Fresh Syrup 100 ml", NewDate(2028, time.June, 30), 100, 1000)
	add("b2", "Watch Tablet 10 mg", NewDate(2027, time.May, 30), 50, 2000)
	add("b3", "Warn Capsule 250 mg", NewDate(2027, time.April, 30), 20, 5000)
	add("b4", "Urgent Drops 5 ml", NewDate(2027, time.March, 15), 10, 3000)
	add("b5", "Dead Ointment 15 g", NewDate(2027, time.February, 28), 25, 4000)
	add("b6", "Open Wide Inhaler", NewDate(2027, time.July, 31), 40, 2500)

	loss := add("b7", "Loss Sachet", NewDate(2027, time.January, 31), 30, 2000)
	loss.WrittenOff = true
	loss.WrittenOffOn = NewDate(2027, time.February, 10)

	saved := add("b8", "Saved Cream 20 g", NewDate(2027, time.September, 30), 20, 1500)
	saved.ReturnedOn = NewDate(2027, time.February, 20)

	return bk
}

func TestNewBoardTiersAndTotals(t *testing.T) {
	board := NewBoard(boardBook(t), boardAsOf)

	if board.AsOf != boardAsOf {
		t.Errorf("AsOf = %s, want %s", board.AsOf, boardAsOf)
	}
	if board.Currency != INR {
		t.Errorf("Currency = %s, want %s", board.Currency, INR)
	}

	t.Run("settled batches are not on the shelf", func(t *testing.T) {
		if len(board.Rows) != 6 {
			t.Fatalf("got %d rows, want 6 live batches", len(board.Rows))
		}
		for _, row := range board.Rows {
			if row.Batch.Settled() {
				t.Errorf("%s is settled and should not be on the board", row.Batch.ID)
			}
		}
	})

	t.Run("totals", func(t *testing.T) {
		cases := []struct {
			name string
			got  int
			want int
		}{
			{"batches", board.Totals.Batches, 6},
			{"units", board.Totals.Units, 245},
			{"products", board.Totals.Products, 6},
			{"cost value", int(board.Totals.CostValue), 530000},
			{"retail value", int(board.Totals.RetailValue), 795000},
		}
		for _, tc := range cases {
			if tc.got != tc.want {
				t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
			}
		}
		if got := board.Totals.CostValue.Format(INR); got != "₹5,300.00" {
			t.Errorf("formatted cost value = %q", got)
		}
	})

	t.Run("buckets are worst first and add up to the totals", func(t *testing.T) {
		want := []struct {
			tier    Tier
			batches int
			units   int
			cost    Money
		}{
			{TierExpired, 1, 25, 100000},
			{TierUrgent, 1, 10, 30000},
			{TierWarn, 1, 20, 100000},
			{TierWatch, 1, 50, 100000},
			{TierFresh, 2, 140, 200000},
		}
		if len(board.Buckets) != len(want) {
			t.Fatalf("got %d buckets, want %d: %+v", len(board.Buckets), len(want), board.Buckets)
		}
		var batches, units int
		var cost Money
		for i, w := range want {
			got := board.Buckets[i]
			if got.Tier != w.tier || got.Batches != w.batches || got.Units != w.units || got.CostValue != w.cost {
				t.Errorf("bucket %d = %s %d batches / %d units / %d, want %s %d / %d / %d",
					i, got.Tier, got.Batches, got.Units, got.CostValue, w.tier, w.batches, w.units, w.cost)
			}
			if got.Label == "" || got.Band == "" {
				t.Errorf("bucket %d has no label or band", i)
			}
			batches += got.Batches
			units += got.Units
			cost += got.CostValue
		}
		if batches != board.Totals.Batches || units != board.Totals.Units || cost != board.Totals.CostValue {
			t.Errorf("the buckets sum to %d/%d/%d but the totals say %d/%d/%d",
				batches, units, cost, board.Totals.Batches, board.Totals.Units, board.Totals.CostValue)
		}
	})

	t.Run("stock about to expire excludes both fresh and dead", func(t *testing.T) {
		// b2 (watch) + b3 (warn) + b4 (urgent): 1,000 + 1,000 + 300.
		cases := []struct {
			name string
			got  int
			want int
		}{
			{"batches", board.AtRisk.Batches, 3},
			{"units", board.AtRisk.Units, 80},
			{"cost value", int(board.AtRisk.CostValue), 230000},
			{"retail value", int(board.AtRisk.RetailValue), 345000},
		}
		for _, tc := range cases {
			if tc.got != tc.want {
				t.Errorf("at risk %s = %d, want %d", tc.name, tc.got, tc.want)
			}
		}
		if board.AtRisk.Label == "" || board.AtRisk.Band != BandAmber {
			t.Errorf("AtRisk = %+v, want a label and the amber band", board.AtRisk)
		}
		if board.AtRisk.CostValue >= board.Totals.CostValue {
			t.Error("stock at risk must be less than all the stock on hand")
		}
	})

	t.Run("rows are ordered worst first, then soonest, then biggest", func(t *testing.T) {
		want := []string{"b5", "b4", "b3", "b2", "b6", "b1"}
		for i, id := range want {
			if board.Rows[i].Batch.ID != id {
				t.Errorf("row %d = %s, want %s (order was %s)", i, board.Rows[i].Batch.ID, id, rowIDs(board.Rows))
				break
			}
		}
	})

	t.Run("every row carries a next action", func(t *testing.T) {
		for _, row := range board.Rows {
			if row.Action == "" {
				t.Errorf("%s has no action", row.Batch.ID)
			}
			if row.Describe == "" {
				t.Errorf("%s has no description", row.Batch.ID)
			}
			if row.CostValue != row.Batch.CostValue() {
				t.Errorf("%s cost value = %d, want %d", row.Batch.ID, row.CostValue, row.Batch.CostValue())
			}
		}
	})
}

func rowIDs(rows []Row) string {
	out := ""
	for _, r := range rows {
		out += r.Batch.ID + " "
	}
	return out
}

func TestNewBoardReturnsMoney(t *testing.T) {
	board := NewBoard(boardBook(t), boardAsOf)
	r := board.Returns

	cases := []struct {
		name string
		got  int
		want int
	}{
		{"open batches", r.OpenBatches, 2},                         // b2 and b6
		{"open cost value", int(r.OpenCostValue), 200000},          // 1,000.00 + 1,000.00
		{"recoverable at full credit", int(r.Recoverable), 200000}, // the default credit rate is 100%
		{"batches closing soon", r.ClosingSoonBatches, 1},          // b2, whose deadline is today
		{"recoverable closing soon", int(r.ClosingSoonRecoverable), 100000},
		{"batches limited by the credit note", r.CreditNoteLimited, 0}, // the cut-off is off in this policy
		{"batches not yet returnable", r.NotYetOpenBatches, 1},         // b1
		{"value not yet returnable", int(r.NotYetOpenCostValue), 100000},
		{"stranded batches", r.StrandedBatches, 2},           // b3 and b4: past the window, still saleable
		{"stranded value", int(r.StrandedCostValue), 130000}, // 1,000.00 + 300.00
		{"dead batches", r.DeadBatches, 1},                   // b5
		{"dead units", r.DeadUnits, 25},
		{"dead value", int(r.DeadCostValue), 100000},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	if r.NextDeadline != boardAsOf {
		t.Errorf("NextDeadline = %s, want %s — b2's window closes today", r.NextDeadline, boardAsOf)
	}

	// Nothing may be counted twice, and every live batch has to land somewhere.
	placed := r.OpenBatches + r.NotYetOpenBatches + r.StrandedBatches + r.DeadBatches
	if placed != board.Totals.Batches {
		t.Errorf("the returns summary places %d batches but there are %d on the board", placed, board.Totals.Batches)
	}
	value := r.OpenCostValue + r.NotYetOpenCostValue + r.StrandedCostValue + r.DeadCostValue
	if value != board.Totals.CostValue {
		t.Errorf("the returns summary accounts for %d but the stock is worth %d", value, board.Totals.CostValue)
	}
	if r.Recoverable > r.OpenCostValue {
		t.Error("recoverable money cannot exceed what the stock cost")
	}
}

func TestRecoverableFollowsTheCreditRate(t *testing.T) {
	cases := []struct {
		creditPercent int
		recoverable   Money
	}{
		{100, 200000},
		{92, 184000},
		{85, 170000},
		{50, 100000},
		{1, 2000},
	}
	for _, tc := range cases {
		bk := boardBook(t)
		bk.Policy.CreditPercent = tc.creditPercent
		if err := bk.Policy.Validate(); err != nil {
			t.Fatalf("credit rate %d: %v", tc.creditPercent, err)
		}
		board := NewBoard(bk, boardAsOf)
		if board.Returns.Recoverable != tc.recoverable {
			t.Errorf("at %d%% credit, recoverable = %d, want %d", tc.creditPercent, board.Returns.Recoverable, tc.recoverable)
		}
		if board.Returns.OpenCostValue != 200000 {
			t.Errorf("the credit rate must not change what the stock cost: %d", board.Returns.OpenCostValue)
		}
	}
}

func TestRecoverableIsZeroWhenNothingCanBeClaimed(t *testing.T) {
	bk := boardBook(t)
	board := NewBoard(bk, boardAsOf)
	for _, row := range board.Rows {
		switch row.Batch.ID {
		case "b2", "b6":
			if row.Recoverable == 0 {
				t.Errorf("%s is inside the window and should carry a recoverable figure", row.Batch.ID)
			}
		default:
			if row.Recoverable != 0 {
				t.Errorf("%s is %s / %s and must not claim %d recoverable",
					row.Batch.ID, row.Verdict.Tier, row.Verdict.Return.State, row.Recoverable)
			}
		}
	}
}

func TestNewBoardLossLog(t *testing.T) {
	board := NewBoard(boardBook(t), boardAsOf)
	loss := board.Loss

	cases := []struct {
		name string
		got  int
		want int
	}{
		{"look-back window", loss.WindowDays, DefaultLossWindowDays},
		{"batches written off", loss.Batches, 1},
		{"units written off", loss.Units, 30},
		{"cost written off", int(loss.CostValue), 60000},
		{"retail written off", int(loss.RetailValue), 90000},
		{"batches rescued", loss.ReturnedBatches, 1},
		{"value rescued", int(loss.ReturnedCostValue), 30000},
		// 600.00 written off against 5,300.00 of stock on hand = 11.3%
		{"share of stock on hand, in tenths of a percent", loss.SharePerMille, 113},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	if want := boardAsOf.AddDays(-DefaultLossWindowDays + 1); loss.Since != want {
		t.Errorf("Since = %s, want %s", loss.Since, want)
	}

	t.Run("a shorter look-back excludes an older write-off", func(t *testing.T) {
		bk := boardBook(t)
		bk.Policy.LossWindowDays = 7 // 23 February to 1 March
		board := NewBoard(bk, boardAsOf)
		if board.Loss.Batches != 0 || board.Loss.CostValue != 0 {
			t.Errorf("a 10 February write-off is outside a seven-day look-back, got %d batches / %d", board.Loss.Batches, board.Loss.CostValue)
		}
		if board.Loss.ReturnedBatches != 0 {
			t.Errorf("a 20 February return is outside it too, got %d", board.Loss.ReturnedBatches)
		}
	})

	t.Run("the window includes its first and last day", func(t *testing.T) {
		bk := NewBook()
		bk.Policy = testPolicy()
		bk.Policy.LossWindowDays = 10 // 20 February to 1 March
		for i, day := range []Date{
			NewDate(2027, time.February, 19), // one day too early
			NewDate(2027, time.February, 20), // the first day
			NewDate(2027, time.March, 1),     // the last day
			NewDate(2027, time.March, 2),     // tomorrow
		} {
			b := Batch{ID: newID(i + 1), Product: "X", BatchNo: newID(i + 1), ExpiresOn: day, Qty: 1, UnitCost: 100, WrittenOff: true, WrittenOffOn: day}
			bk.Batches = append(bk.Batches, b)
		}
		bk.Seq = len(bk.Batches)
		loss := NewLoss(bk, boardAsOf, 0)
		if loss.Batches != 2 {
			t.Errorf("counted %d write-offs, want the two inside the window", loss.Batches)
		}
		if loss.SharePerMille != 0 {
			t.Errorf("SharePerMille = %d; with no stock on hand there is no ratio to report", loss.SharePerMille)
		}
	})

	t.Run("a write-off with no date is not counted", func(t *testing.T) {
		bk := NewBook()
		bk.Policy = testPolicy()
		bk.Batches = []Batch{{ID: "b1", Product: "X", BatchNo: "X1", ExpiresOn: NewDate(2027, time.January, 1), Qty: 5, UnitCost: 100, WrittenOff: true}}
		if loss := NewLoss(bk, boardAsOf, 100000); loss.Batches != 0 {
			t.Errorf("counted %d, want 0 — an undated write-off cannot be placed in a window", loss.Batches)
		}
	})
}

func TestBoardOnAnEmptyBook(t *testing.T) {
	board := NewBoard(NewBook(), boardAsOf)
	if board.Rows == nil || len(board.Rows) != 0 {
		t.Errorf("Rows = %v, want an empty slice rather than nil", board.Rows)
	}
	if board.Buckets == nil || len(board.Buckets) != 0 {
		t.Errorf("Buckets = %v, want an empty slice", board.Buckets)
	}
	if board.Totals != (Totals{}) {
		t.Errorf("Totals = %+v, want everything zero", board.Totals)
	}
	if board.Returns != (Returns{}) {
		t.Errorf("Returns = %+v, want everything zero", board.Returns)
	}
	if board.Loss.SharePerMille != 0 {
		t.Errorf("SharePerMille = %d, want 0 with no stock", board.Loss.SharePerMille)
	}
	if !board.Returns.NextDeadline.IsZero() {
		t.Errorf("NextDeadline = %s, want the zero date", board.Returns.NextDeadline)
	}
}

func TestBoardDefaultsToToday(t *testing.T) {
	if got := NewBoard(NewBook(), Date{}); got.AsOf != Today() {
		t.Errorf("AsOf = %s, want today", got.AsOf)
	}
}

func TestReturnList(t *testing.T) {
	bk := boardBook(t)
	list := ReturnList(bk, boardAsOf)

	t.Run("only what can be acted on, earliest deadline first", func(t *testing.T) {
		want := []string{"b2", "b6"}
		if len(list) != len(want) {
			t.Fatalf("got %d rows (%s), want %d", len(list), rowIDs(list), len(want))
		}
		for i, id := range want {
			if list[i].Batch.ID != id {
				t.Errorf("row %d = %s, want %s", i, list[i].Batch.ID, id)
			}
		}
		for i := 1; i < len(list); i++ {
			if list[i].Verdict.Return.Deadline.Before(list[i-1].Verdict.Return.Deadline) {
				t.Error("the list is not in deadline order")
			}
		}
	})

	t.Run("expired, stranded and distant stock is left out", func(t *testing.T) {
		for _, row := range list {
			if row.Verdict.Tier == TierExpired {
				t.Errorf("%s is expired and cannot go back", row.Batch.ID)
			}
			if row.Verdict.Return.State == ReturnClosed {
				t.Errorf("%s has a closed window", row.Batch.ID)
			}
		}
	})

	t.Run("stock whose window opens within the notice period is included", func(t *testing.T) {
		// b1's window opens on 2 January 2028. Ask on the day a fortnight
		// before that and it should appear, with nothing recoverable yet.
		asOf := NewDate(2027, time.December, 20)
		got := ReturnList(bk, asOf)
		var found bool
		for _, row := range got {
			if row.Batch.ID == "b1" {
				found = true
				if row.Verdict.Return.State != ReturnTooEarly {
					t.Errorf("state = %s, want %s", row.Verdict.Return.State, ReturnTooEarly)
				}
				if row.Recoverable != 0 {
					t.Errorf("Recoverable = %d, want 0 until the window opens", row.Recoverable)
				}
			}
		}
		if !found {
			t.Errorf("b1 should be on the list on %s; got %s", asOf, rowIDs(got))
		}
	})

	t.Run("an empty book gives an empty list, not nil", func(t *testing.T) {
		if got := ReturnList(NewBook(), boardAsOf); got == nil {
			t.Error("nil marshals as null instead of []")
		}
	})

	t.Run("a batch with nothing left on hand is not worth returning", func(t *testing.T) {
		bk := boardBook(t)
		bk.Find("b2").Qty = 0
		for _, row := range ReturnList(bk, boardAsOf) {
			if row.Batch.ID == "b2" {
				t.Error("an empty batch is still on the returns list")
			}
		}
	})
}

func TestShelfOrder(t *testing.T) {
	bk := NewBook()
	bk.Policy = testPolicy()
	bk.Batches = []Batch{
		lot("b1", "LATER", NewDate(2028, time.March, 31), 50),
		lot("b2", "SOONEST", NewDate(2027, time.April, 30), 20),
		lot("b3", "MIDDLE", NewDate(2027, time.December, 31), 30),
	}
	other := lot("b4", "OTHER", NewDate(2027, time.April, 1), 10)
	other.Product = "Paracetamol 650 mg"
	bk.Batches = append(bk.Batches, other)
	settled := lot("b5", "GONE", NewDate(2027, time.April, 2), 10)
	settled.WrittenOff = true
	settled.WrittenOffOn = NewDate(2027, time.February, 1)
	bk.Batches = append(bk.Batches, settled)
	bk.Seq = 5

	got := ShelfOrder(bk, "amoxicillin 500 mg", boardAsOf)
	want := []string{"SOONEST", "MIDDLE", "LATER"}
	if len(got) != len(want) {
		t.Fatalf("got %d rows (%s), want %d", len(got), rowIDs(got), len(want))
	}
	for i, batchNo := range want {
		if got[i].Batch.BatchNo != batchNo {
			t.Errorf("row %d = %s, want %s", i, got[i].Batch.BatchNo, batchNo)
		}
		if got[i].Action == "" {
			t.Errorf("row %d has no action", i)
		}
	}

	t.Run("an unknown product gives an empty list", func(t *testing.T) {
		if got := ShelfOrder(bk, "Nothing At All", boardAsOf); len(got) != 0 {
			t.Errorf("got %d rows", len(got))
		}
	})
}

func TestBoardWithTheCreditNoteCutoffOn(t *testing.T) {
	bk := boardBook(t)
	bk.Policy = DefaultPolicy() // India, cut-off applied
	// b6's window closes on 2 May 2027. Bought in the year to 31 March 2026,
	// its credit note was due by 30 November 2026 — already gone.
	bk.Find("b6").PurchasedOn = NewDate(2025, time.July, 1)
	// b2 was bought this financial year, so the window still governs it.
	bk.Find("b2").PurchasedOn = NewDate(2026, time.July, 1)

	board := NewBoard(bk, boardAsOf)
	if board.Returns.OpenBatches != 1 {
		t.Errorf("open batches = %d, want 1 — b6's credit note can no longer be raised", board.Returns.OpenBatches)
	}
	if board.Returns.Recoverable != 100000 {
		t.Errorf("recoverable = %d, want 100000 (b2 only)", board.Returns.Recoverable)
	}
	if board.Returns.StrandedBatches != 3 {
		t.Errorf("stranded = %d, want 3 — b3, b4 and now b6", board.Returns.StrandedBatches)
	}
	for _, row := range board.Rows {
		if row.Batch.ID == "b6" && row.Recoverable != 0 {
			t.Errorf("b6 still claims %d recoverable after the cut-off passed", row.Recoverable)
		}
	}
}
