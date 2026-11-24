package stock

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// FEFO — first expiry, first out — is the only sane picking rule for stock with
// an expiry date, and it is not the same as first in, first out. A consignment
// that arrived last week can easily carry an earlier expiry than one that has
// been on the shelf for a month, so picking by arrival order quietly ages the
// short-dated lot into a loss. These functions answer the counter question:
// of the batches of this product, which do I hand over, and in what order?

// Allocation is one line of a pick list: take this many units from this batch.
type Allocation struct {
	BatchID   string  `json:"batchId"`
	BatchNo   string  `json:"batchNo"`
	Shelf     string  `json:"shelf"`
	ExpiresOn Date    `json:"expiresOn"`
	Take      int     `json:"take"`
	Remaining int     `json:"remaining"`
	Tier      Tier    `json:"tier"`
	Band      string  `json:"band"`
	DaysLeft  int     `json:"daysLeft"`
	Value     Money   `json:"value"`
	Verdict   Verdict `json:"verdict"`
}

// Skipped is a batch that was passed over, and why.
type Skipped struct {
	BatchID   string `json:"batchId"`
	BatchNo   string `json:"batchNo"`
	ExpiresOn Date   `json:"expiresOn"`
	Qty       int    `json:"qty"`
	Reason    string `json:"reason"`
}

// PickList is the answer to "give me N of this product".
type PickList struct {
	Product   string       `json:"product"`
	AsOf      Date         `json:"asOf"`
	Requested int          `json:"requested"`
	Allocated int          `json:"allocated"`
	Shortfall int          `json:"shortfall"`
	Lines     []Allocation `json:"lines"`
	Skipped   []Skipped    `json:"skipped"`
	// Available is every dispensable unit of the product on hand, whether or
	// not it was needed for this request.
	Available int `json:"available"`
}

// Complete reports whether the whole request could be filled.
func (pl PickList) Complete() bool { return pl.Shortfall == 0 && pl.Requested > 0 }

// FEFOLess is the ordering: earliest expiry first, and for two batches that
// expire on the same day the one bought earlier goes first, then batch number,
// then id. The last two tie-breaks exist so the same stock always produces the
// same pick slip — staff stop trusting a list that reshuffles itself.
func FEFOLess(a, b Batch) bool {
	if !a.ExpiresOn.Time().Equal(b.ExpiresOn.Time()) {
		return a.ExpiresOn.Before(b.ExpiresOn)
	}
	if !a.PurchasedOn.Time().Equal(b.PurchasedOn.Time()) {
		return a.PurchasedOn.Before(b.PurchasedOn)
	}
	if a.BatchNo != b.BatchNo {
		return a.BatchNo < b.BatchNo
	}
	return idNumber(a.ID) < idNumber(b.ID)
}

// FEFOOrder returns the batches sorted into picking order. The input is not
// modified.
func FEFOOrder(batches []Batch) []Batch {
	out := make([]Batch, len(batches))
	copy(out, batches)
	sort.SliceStable(out, func(i, j int) bool { return FEFOLess(out[i], out[j]) })
	return out
}

// Pick builds the pick list for a quantity of one product.
//
// Expired batches are never allocated: selling or dispensing stock past its
// labelled expiry is prohibited, so they are listed as skipped with the reason
// rather than silently dropped — somebody has to go and physically remove them.
// Batches already returned or written off are skipped for the same reason.
func Pick(batches []Batch, product string, qty int, asOf Date, p Policy) (PickList, error) {
	product = squash(product)
	if product == "" {
		return PickList{}, errors.New("choose a product first")
	}
	if qty <= 0 {
		return PickList{}, errors.New("the quantity to pick must be at least 1")
	}
	if asOf.IsZero() {
		asOf = Today()
	}

	pl := PickList{Product: product, AsOf: asOf, Requested: qty, Lines: []Allocation{}, Skipped: []Skipped{}}
	want := qty
	matched := false

	for _, b := range FEFOOrder(batches) {
		if !strings.EqualFold(b.Product, product) {
			continue
		}
		matched = true
		v := Grade(b.ExpiresOn, b.PurchasedOn, asOf, p)

		switch {
		case b.WrittenOff:
			pl.Skipped = append(pl.Skipped, skip(b, "written off as a loss"))
			continue
		case !b.ReturnedOn.IsZero():
			pl.Skipped = append(pl.Skipped, skip(b, "returned to the distributor on "+b.ReturnedOn.Pretty()))
			continue
		case !v.Tier.Dispensable():
			pl.Skipped = append(pl.Skipped, skip(b, v.Describe()+" — may not be sold, remove it from the shelf"))
			continue
		case b.Qty <= 0:
			pl.Skipped = append(pl.Skipped, skip(b, "nothing left on hand"))
			continue
		}

		pl.Available += b.Qty
		if want <= 0 {
			continue
		}
		take := b.Qty
		if take > want {
			take = want
		}
		want -= take
		pl.Allocated += take
		pl.Lines = append(pl.Lines, Allocation{
			BatchID:   b.ID,
			BatchNo:   b.BatchNo,
			Shelf:     b.Shelf,
			ExpiresOn: b.ExpiresOn,
			Take:      take,
			Remaining: b.Qty - take,
			Tier:      v.Tier,
			Band:      v.Band,
			DaysLeft:  v.DaysLeft,
			Value:     Times(b.UnitCost, take),
			Verdict:   v,
		})
	}

	if !matched {
		return PickList{}, fmt.Errorf("no batches of %q are on file", product)
	}
	pl.Shortfall = want
	return pl, nil
}

func skip(b Batch, reason string) Skipped {
	return Skipped{BatchID: b.ID, BatchNo: b.BatchNo, ExpiresOn: b.ExpiresOn, Qty: b.Qty, Reason: reason}
}

// ApplyPick deducts a pick list from the stock on hand.
//
// It records a movement of stock and nothing else: no customer, no prescriber,
// no price, no sale. This tool is not a sale register and keeps nothing that
// would make it one; it only keeps the count honest, because a quantity nobody
// ever decrements turns the money columns into fiction.
//
// The deduction is all or nothing. Every line is re-checked against the book
// first — the quantity may have changed in another tab since the list was
// built — and if any line no longer fits, nothing is deducted at all.
func (bk *Book) ApplyPick(pl PickList) error {
	if len(pl.Lines) == 0 {
		return errors.New("there is nothing in this pick list to take off the shelf")
	}
	type change struct {
		batch *Batch
		take  int
	}
	changes := make([]change, 0, len(pl.Lines))
	for _, line := range pl.Lines {
		b := bk.Find(line.BatchID)
		if b == nil {
			return fmt.Errorf("batch %s is no longer on file; rebuild the pick list", line.BatchNo)
		}
		if line.Take <= 0 {
			return fmt.Errorf("batch %s has nothing to take", line.BatchNo)
		}
		if b.Settled() {
			return fmt.Errorf("batch %s has been returned or written off; rebuild the pick list", line.BatchNo)
		}
		if b.Qty < line.Take {
			return fmt.Errorf("batch %s has only %d left, not %d; rebuild the pick list", line.BatchNo, b.Qty, line.Take)
		}
		changes = append(changes, change{batch: b, take: line.Take})
	}
	for _, c := range changes {
		c.batch.Qty -= c.take
	}
	return nil
}
