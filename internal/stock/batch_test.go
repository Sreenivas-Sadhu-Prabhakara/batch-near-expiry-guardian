package stock

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// sample is a well-formed batch the tests bend one field at a time.
func sample() Batch {
	return Batch{
		Product:     "Amoxicillin 500 mg capsule",
		Pack:        "strip of 10",
		BatchNo:     "AX2291",
		ExpiresOn:   NewDate(2027, time.June, 30),
		PurchasedOn: NewDate(2026, time.August, 1),
		Qty:         180,
		UnitCost:    Money(1210),
		UnitPrice:   Money(1800),
		Supplier:    "Metro Distributors",
		Shelf:       "A3",
	}
}

func TestBatchValidate(t *testing.T) {
	cases := []struct {
		name string
		bend func(b *Batch)
		want string
	}{
		{"the sample", func(b *Batch) {}, ""},
		{"no product", func(b *Batch) { b.Product = "" }, "product name is needed"},
		{"an over-long product", func(b *Batch) { b.Product = strings.Repeat("x", maxFieldLen+1) }, "longer than"},
		{"a product at the limit", func(b *Batch) { b.Product = strings.Repeat("x", maxFieldLen) }, ""},
		{"no batch number", func(b *Batch) { b.BatchNo = "" }, "batch number"},
		{"an over-long batch number", func(b *Batch) { b.BatchNo = strings.Repeat("A", 41) }, "longer than 40"},
		{"no expiry", func(b *Batch) { b.ExpiresOn = Date{} }, "expiry date is needed"},
		{"a negative quantity", func(b *Batch) { b.Qty = -1 }, "cannot be negative"},
		{"nothing on hand is fine", func(b *Batch) { b.Qty = 0 }, ""},
		{"a negative unit cost", func(b *Batch) { b.UnitCost = -1 }, "unit cost cannot be negative"},
		{"a negative unit price", func(b *Batch) { b.UnitPrice = -1 }, "unit price cannot be negative"},
		{"no purchase date is fine", func(b *Batch) { b.PurchasedOn = Date{} }, ""},
		{"a purchase date after the expiry", func(b *Batch) { b.PurchasedOn = NewDate(2028, time.January, 1) }, "after the expiry date"},
		{"a purchase date on the expiry", func(b *Batch) { b.PurchasedOn = b.ExpiresOn }, ""},
		{"an over-long shelf", func(b *Batch) { b.Shelf = strings.Repeat("x", maxFieldLen+1) }, "longer than"},
		{"an over-long note", func(b *Batch) { b.Note = strings.Repeat("x", 501) }, "note is longer"},
		{"a note at the limit", func(b *Batch) { b.Note = strings.Repeat("x", 500) }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := sample()
			tc.bend(&b)
			err := b.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestBatchNormalise(t *testing.T) {
	b := Batch{
		Product:   "  Amoxicillin   500 mg\tcapsule  ",
		Pack:      " strip of  10 ",
		BatchNo:   " ax2291 ",
		Supplier:  "  Metro   Distributors ",
		Shelf:     " a3 ",
		Note:      "  near  the  counter ",
		Qty:       -5,
		ExpiresOn: NewDate(2027, time.June, 30),
	}
	b.Normalise()

	if b.Product != "Amoxicillin 500 mg capsule" {
		t.Errorf("Product = %q", b.Product)
	}
	if b.Pack != "strip of 10" {
		t.Errorf("Pack = %q", b.Pack)
	}
	if b.BatchNo != "AX2291" {
		t.Errorf("BatchNo = %q, want it uppercased", b.BatchNo)
	}
	if b.Supplier != "Metro Distributors" {
		t.Errorf("Supplier = %q", b.Supplier)
	}
	if b.Shelf != "a3" {
		t.Errorf("Shelf = %q, want the shop's own casing kept", b.Shelf)
	}
	if b.Note != "near the counter" {
		t.Errorf("Note = %q", b.Note)
	}
	if b.Qty != 0 {
		t.Errorf("Qty = %d, want a negative quantity clamped to 0", b.Qty)
	}

	t.Run("writing off stamps the date", func(t *testing.T) {
		w := sample()
		w.WrittenOff = true
		w.Normalise()
		if w.WrittenOffOn.IsZero() {
			t.Error("a written-off batch must carry a date")
		}
	})
	t.Run("clearing a write-off clears the date", func(t *testing.T) {
		w := sample()
		w.WrittenOff = false
		w.WrittenOffOn = NewDate(2027, time.January, 1)
		w.Normalise()
		if !w.WrittenOffOn.IsZero() {
			t.Errorf("WrittenOffOn = %s, want it cleared", w.WrittenOffOn)
		}
	})
}

func TestBatchKeyIgnoresCaseAndSpacing(t *testing.T) {
	a := Batch{Product: "Amoxicillin 500", BatchNo: "AX2291"}
	b := Batch{Product: "amoxicillin   500", BatchNo: "ax2291"}
	c := Batch{Product: "Amoxicillin 250", BatchNo: "AX2291"}
	d := Batch{Product: "Amoxicillin 500", BatchNo: "AX2292"}
	if a.Key() != b.Key() {
		t.Error("the same lot typed differently must be one key")
	}
	if a.Key() == c.Key() {
		t.Error("the same batch number on a different product is a different lot")
	}
	if a.Key() == d.Key() {
		t.Error("a different batch number is a different lot")
	}
}

func TestBatchValues(t *testing.T) {
	cases := []struct {
		name     string
		qty      int
		unitCost Money
		unitPr   Money
		cost     Money
		retail   Money
		margin   Money
	}{
		{"a full strip count", 180, 1210, 1800, 217800, 324000, 106200},
		{"one unit", 1, 1210, 1800, 1210, 1800, 590},
		{"nothing on hand", 0, 1210, 1800, 0, 0, 0},
		{"free goods", 50, 0, 1800, 0, 90000, 90000},
		{"priced below cost", 10, 1800, 1210, 18000, 12100, -5900},
		{"no price recorded", 10, 1210, 0, 12100, 0, -12100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := Batch{Qty: tc.qty, UnitCost: tc.unitCost, UnitPrice: tc.unitPr}
			if got := b.CostValue(); got != tc.cost {
				t.Errorf("CostValue() = %d, want %d", got, tc.cost)
			}
			if got := b.RetailValue(); got != tc.retail {
				t.Errorf("RetailValue() = %d, want %d", got, tc.retail)
			}
			if got := b.Margin(); got != tc.margin {
				t.Errorf("Margin() = %d, want %d", got, tc.margin)
			}
		})
	}
}

func TestBatchSettledAndLive(t *testing.T) {
	cases := []struct {
		name    string
		bend    func(b *Batch)
		settled bool
		live    bool
	}{
		{"on the shelf", func(b *Batch) {}, false, true},
		{"nothing left", func(b *Batch) { b.Qty = 0 }, false, false},
		{"written off", func(b *Batch) { b.WrittenOff = true }, true, false},
		{"returned", func(b *Batch) { b.ReturnedOn = NewDate(2027, time.March, 1) }, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := sample()
			tc.bend(&b)
			if got := b.Settled(); got != tc.settled {
				t.Errorf("Settled() = %v, want %v", got, tc.settled)
			}
			if got := b.Live(); got != tc.live {
				t.Errorf("Live() = %v, want %v", got, tc.live)
			}
		})
	}
}

func TestBatchWarnings(t *testing.T) {
	p := DefaultPolicy()
	cases := []struct {
		name string
		bend func(b *Batch)
		want string
	}{
		{"a clean batch", func(b *Batch) {}, ""},
		{"no purchase date", func(b *Batch) { b.PurchasedOn = Date{} }, "credit-note cut-off cannot be worked out"},
		{"zero cost", func(b *Batch) { b.UnitCost = 0 }, "contributes nothing to the money at risk"},
		{"price below cost", func(b *Batch) { b.UnitPrice = 1000 }, "below the unit cost"},
		{"an expiry a century out", func(b *Batch) { b.ExpiresOn = Today().AddMonths(12 * 50) }, "more than ten years out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := sample()
			tc.bend(&b)
			got := b.Warnings(p)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("want no warnings, got %v", got)
				}
				return
			}
			joined := strings.Join(got, " | ")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("warnings = %q, want one containing %q", joined, tc.want)
			}
		})
	}
	t.Run("no cut-off warning outside india", func(t *testing.T) {
		ph := DefaultPolicy()
		ph.Market = Philippines
		ph.Normalise()
		b := sample()
		b.PurchasedOn = Date{}
		for _, w := range b.Warnings(ph) {
			if strings.Contains(w, "credit-note") {
				t.Errorf("a Philippine shop should not be warned about the Indian cut-off: %q", w)
			}
		}
	})
}

func TestBookUpsert(t *testing.T) {
	bk := NewBook()

	first, err := bk.Upsert(sample())
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if first.ID != "b1" {
		t.Errorf("first id = %q, want b1", first.ID)
	}
	if len(bk.Batches) != 1 {
		t.Fatalf("book holds %d batches", len(bk.Batches))
	}

	t.Run("a second lot of the same product is fine", func(t *testing.T) {
		second := sample()
		second.BatchNo = "AX3310"
		second.ExpiresOn = NewDate(2028, time.March, 31)
		got, err := bk.Upsert(second)
		if err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if got.ID != "b2" {
			t.Errorf("second id = %q, want b2", got.ID)
		}
	})

	t.Run("the same lot twice is refused", func(t *testing.T) {
		dup := sample()
		dup.BatchNo = "ax2291" // same lot, typed in lower case
		_, err := bk.Upsert(dup)
		if err == nil {
			t.Fatal("a second row for one lot must be refused")
		}
		if !strings.Contains(err.Error(), "b1") {
			t.Errorf("the error should point at the existing row: %v", err)
		}
	})

	t.Run("an edit keeps the id", func(t *testing.T) {
		edit := first
		edit.Qty = 120
		edit.Shelf = "B1"
		got, err := bk.Upsert(edit)
		if err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if got.ID != "b1" {
			t.Errorf("id = %q, want b1", got.ID)
		}
		if bk.Find("b1").Qty != 120 {
			t.Errorf("quantity not saved: %d", bk.Find("b1").Qty)
		}
		if len(bk.Batches) != 2 {
			t.Errorf("an edit added a row: %d batches", len(bk.Batches))
		}
	})

	t.Run("editing an unknown id fails", func(t *testing.T) {
		ghost := sample()
		ghost.ID = "b99"
		ghost.BatchNo = "ZZ1"
		if _, err := bk.Upsert(ghost); !errors.Is(err, ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("an invalid batch is refused before anything changes", func(t *testing.T) {
		before := len(bk.Batches)
		bad := sample()
		bad.BatchNo = "QQ1"
		bad.ExpiresOn = Date{}
		if _, err := bk.Upsert(bad); err == nil {
			t.Fatal("want an error")
		}
		if len(bk.Batches) != before {
			t.Error("a refused batch was still added")
		}
	})

	t.Run("fields are normalised on the way in", func(t *testing.T) {
		messy := sample()
		messy.BatchNo = "  zz-7  "
		messy.Product = "  Paracetamol  650 mg "
		got, err := bk.Upsert(messy)
		if err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if got.BatchNo != "ZZ-7" || got.Product != "Paracetamol 650 mg" {
			t.Errorf("stored as %q / %q", got.Product, got.BatchNo)
		}
	})
}

func TestBookDelete(t *testing.T) {
	bk := NewBook()
	a, _ := bk.Upsert(sample())
	second := sample()
	second.BatchNo = "AX3310"
	b, _ := bk.Upsert(second)

	if err := bk.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if bk.Find(a.ID) != nil {
		t.Error("the batch is still there")
	}
	if bk.Find(b.ID) == nil {
		t.Error("the wrong batch was deleted")
	}
	if err := bk.Delete(a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting twice = %v, want ErrNotFound", err)
	}

	t.Run("an id is never reused after a deletion", func(t *testing.T) {
		next, err := bk.Upsert(func() Batch { s := sample(); s.BatchNo = "NEW1"; return s }())
		if err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		if next.ID == a.ID {
			t.Errorf("id %q was handed out again", next.ID)
		}
	})
}

func TestBookWriteOffReturnAndReopen(t *testing.T) {
	bk := NewBook()
	b, _ := bk.Upsert(sample())
	day := NewDate(2027, time.July, 5)

	if err := bk.WriteOff(b.ID, day); err != nil {
		t.Fatalf("WriteOff: %v", err)
	}
	got := bk.Find(b.ID)
	if !got.WrittenOff || got.WrittenOffOn != day {
		t.Errorf("write-off = %v on %s", got.WrittenOff, got.WrittenOffOn)
	}
	if got.Qty != sample().Qty {
		t.Error("a write-off must keep the quantity so the loss stays countable")
	}
	if err := bk.WriteOff(b.ID, day); err == nil {
		t.Error("writing off twice should be refused")
	}
	if err := bk.MarkReturned(b.ID, day); err == nil {
		t.Error("a written-off batch should not also be marked returned")
	}

	if err := bk.Reopen(b.ID); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if got = bk.Find(b.ID); got.WrittenOff || !got.WrittenOffOn.IsZero() || !got.ReturnedOn.IsZero() {
		t.Errorf("Reopen left %+v", got)
	}

	if err := bk.MarkReturned(b.ID, day); err != nil {
		t.Fatalf("MarkReturned: %v", err)
	}
	if bk.Find(b.ID).ReturnedOn != day {
		t.Errorf("ReturnedOn = %s, want %s", bk.Find(b.ID).ReturnedOn, day)
	}
	if err := bk.WriteOff(b.ID, day); err == nil {
		t.Error("a returned batch is not a loss and should not be writable off")
	}

	t.Run("an unknown id", func(t *testing.T) {
		for name, err := range map[string]error{
			"WriteOff":     bk.WriteOff("b99", day),
			"MarkReturned": bk.MarkReturned("b99", day),
			"Reopen":       bk.Reopen("b99"),
		} {
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("%s = %v, want ErrNotFound", name, err)
			}
		}
	})

	t.Run("a missing date defaults to today", func(t *testing.T) {
		fresh := NewBook()
		x, _ := fresh.Upsert(sample())
		if err := fresh.WriteOff(x.ID, Date{}); err != nil {
			t.Fatalf("WriteOff: %v", err)
		}
		if fresh.Find(x.ID).WrittenOffOn != Today() {
			t.Errorf("WrittenOffOn = %s, want today", fresh.Find(x.ID).WrittenOffOn)
		}
	})
}

func TestBookNormaliseRepairsAFileEditedByHand(t *testing.T) {
	bk := &Book{
		Batches: []Batch{
			{Product: " Metformin 500 ", BatchNo: " mf1 ", ExpiresOn: NewDate(2027, time.June, 30)},
			{ID: "b7", Product: "Losartan 50", BatchNo: "LS9", ExpiresOn: NewDate(2027, time.July, 31)},
			{Product: "Atorvastatin 10", BatchNo: "AT4", ExpiresOn: NewDate(2027, time.August, 31)},
		},
	}
	bk.Normalise()

	if bk.Policy.WatchDays != DefaultWatchDays {
		t.Error("the policy was not filled in")
	}
	if bk.Batches[0].Product != "Metformin 500" || bk.Batches[0].BatchNo != "MF1" {
		t.Errorf("fields were not tidied: %+v", bk.Batches[0])
	}
	ids := map[string]bool{}
	for _, b := range bk.Batches {
		if b.ID == "" {
			t.Fatalf("%s has no id", b.Product)
		}
		if ids[b.ID] {
			t.Fatalf("id %q was handed out twice", b.ID)
		}
		ids[b.ID] = true
	}
	if bk.Batches[1].ID != "b7" {
		t.Errorf("an existing id was changed to %q", bk.Batches[1].ID)
	}
	if bk.Seq < 7 {
		t.Errorf("Seq = %d; it must pass every id already in use or the next save collides", bk.Seq)
	}
	next, err := bk.Upsert(sample())
	if err != nil {
		t.Fatalf("Upsert after repair: %v", err)
	}
	if ids[next.ID] {
		t.Errorf("the new id %q collides with an existing one", next.ID)
	}

	t.Run("a nil batch list becomes an empty one", func(t *testing.T) {
		empty := &Book{}
		empty.Normalise()
		if empty.Batches == nil {
			t.Error("Batches is still nil, which marshals as null instead of []")
		}
	})
}

func TestBookProductsAndSuppliers(t *testing.T) {
	bk := NewBook()
	add := func(product, batchNo, supplier string) {
		b := sample()
		b.Product, b.BatchNo, b.Supplier = product, batchNo, supplier
		if _, err := bk.Upsert(b); err != nil {
			t.Fatalf("Upsert(%s/%s): %v", product, batchNo, err)
		}
	}
	add("Paracetamol 650 mg", "P1", "Metro Distributors")
	add("Amoxicillin 500 mg", "A1", "Southern Pharma")
	add("paracetamol  650 mg", "P2", "metro distributors")
	add("Cetirizine 10 mg", "C1", "")

	products := bk.Products()
	if len(products) != 3 {
		t.Errorf("Products() = %v, want three distinct names", products)
	}
	if products[0] != "Amoxicillin 500 mg" {
		t.Errorf("Products() should be sorted, got %v", products)
	}
	suppliers := bk.Suppliers()
	if len(suppliers) != 2 {
		t.Errorf("Suppliers() = %v, want two; the blank one should be dropped", suppliers)
	}

	t.Run("an empty book gives empty lists, not nil", func(t *testing.T) {
		empty := NewBook()
		if empty.Products() == nil || empty.Suppliers() == nil {
			t.Error("nil slices marshal as null instead of []")
		}
	})
}

func TestDescribeBatch(t *testing.T) {
	cases := []struct {
		name string
		bend func(b *Batch)
		want string
	}{
		{"with a pack", func(b *Batch) {}, "Amoxicillin 500 mg capsule (strip of 10) · batch AX2291"},
		{"without a pack", func(b *Batch) { b.Pack = "" }, "Amoxicillin 500 mg capsule · batch AX2291"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := sample()
			tc.bend(&b)
			if got := b.Describe(); got != tc.want {
				t.Errorf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}
