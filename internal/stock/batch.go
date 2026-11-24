package stock

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// maxFieldLen keeps a hand-typed field from becoming a wall of text in a table
// cell. It is generous: the longest real product description — molecule,
// strength, form and brand — fits inside it several times over.
const maxFieldLen = 160

// Batch is one lot of one product sitting on the shelf: a batch number, the
// expiry printed on the carton, how many units are left and what they cost.
//
// This is the record a spreadsheet does not keep. A spreadsheet row says
// "Amoxicillin 500 — 240 capsules"; it cannot tell you that 180 of them are
// batch AX2291 which dies in March and 60 are batch AX3310 which has another
// year, so it cannot tell you what to return, what to sell first, or what you
// are about to lose.
type Batch struct {
	// ID is assigned when the batch is first saved and never changes.
	ID string `json:"id"`
	// Product is the name as the shop knows it, with strength and form.
	Product string `json:"product"`
	// Pack is the unit being counted — "strip of 10", "bottle 60 ml", "box".
	Pack string `json:"pack"`
	// BatchNo is the batch or lot number on the carton, held uppercase because
	// that is how it is printed and "ax2291" is the same batch as "AX2291".
	BatchNo string `json:"batchNo"`
	// ExpiresOn is the last day the stock may be sold. For a pack labelled with
	// only a month and year this is the last day of that month.
	ExpiresOn Date `json:"expiresOn"`
	// PurchasedOn is the date on the purchase invoice. It drives the
	// credit-note cut-off, and a batch without it simply has no cut-off worked
	// out rather than a guessed one.
	PurchasedOn Date `json:"purchasedOn"`
	// Qty is the number of units on hand.
	Qty int `json:"qty"`
	// UnitCost is what one unit cost, net of trade discount. This is the number
	// the loss is measured in: it is what leaves the till if the batch dies.
	UnitCost Money `json:"unitCost"`
	// UnitPrice is the printed retail price of one unit — the MRP in India, the
	// SRP in the Philippines. It measures the sale that will not happen.
	UnitPrice Money `json:"unitPrice"`
	// Supplier is the distributor the batch came from and the one who would
	// issue the credit note.
	Supplier string `json:"supplier"`
	// Shelf is where it physically is, so somebody can go and fetch it.
	Shelf string `json:"shelf"`
	// Note is anything else worth remembering about this lot.
	Note string `json:"note"`
	// WrittenOff marks a batch already taken off the shelf and accounted for as
	// a loss. It leaves the live board but stays in the file, so last year's
	// expiry loss is still countable.
	WrittenOff bool `json:"writtenOff"`
	// WrittenOffOn is when that happened.
	WrittenOffOn Date `json:"writtenOffOn"`
	// ReturnedOn records that the batch went back to the distributor, so a
	// batch already dealt with stops being nagged about.
	ReturnedOn Date `json:"returnedOn"`
}

// CostValue is what the units still on hand cost to buy: the money at stake.
func (b Batch) CostValue() Money { return Times(b.UnitCost, b.Qty) }

// RetailValue is what those units would have sold for.
func (b Batch) RetailValue() Money { return Times(b.UnitPrice, b.Qty) }

// Margin is the gross profit that dies with the batch. It can be negative if a
// unit price has been entered below cost, which Warnings points out rather than
// hiding.
func (b Batch) Margin() Money { return b.RetailValue() - b.CostValue() }

// Settled reports whether the batch has already been returned or written off,
// in which case it no longer needs an action.
func (b Batch) Settled() bool { return b.WrittenOff || !b.ReturnedOn.IsZero() }

// Live reports whether the batch is still on the shelf in a quantity that
// matters.
func (b Batch) Live() bool { return !b.Settled() && b.Qty > 0 }

// Key identifies a batch of a product. The same batch number can appear on two
// different products, and the same product arrives in many batches, so neither
// alone is an identity.
func (b Batch) Key() string {
	return strings.ToLower(squash(b.Product)) + "\x00" + strings.ToUpper(squash(b.BatchNo))
}

// Describe names the batch in one line for a list or a pick slip.
func (b Batch) Describe() string {
	out := b.Product
	if b.Pack != "" {
		out += " (" + b.Pack + ")"
	}
	return out + " · batch " + b.BatchNo
}

// Normalise tidies the fields a person typed: whitespace collapsed, batch
// number uppercased, a written-off batch given a date if it is missing one.
func (b *Batch) Normalise() {
	b.Product = squash(b.Product)
	b.Pack = squash(b.Pack)
	b.BatchNo = strings.ToUpper(squash(b.BatchNo))
	b.Supplier = squash(b.Supplier)
	b.Shelf = squash(b.Shelf)
	b.Note = squash(b.Note)
	if b.Qty < 0 {
		b.Qty = 0
	}
	if b.WrittenOff && b.WrittenOffOn.IsZero() {
		b.WrittenOffOn = Today()
	}
	if !b.WrittenOff {
		b.WrittenOffOn = Date{}
	}
}

// ErrNotFound is returned when an id does not match any batch.
var ErrNotFound = errors.New("no batch with that id")

// Validate checks a batch a person just typed. It is deliberately strict about
// the three fields that make the tool work at all — the product, the batch
// number and the expiry — because a row missing any of them is a spreadsheet
// row again.
func (b Batch) Validate() error {
	switch {
	case b.Product == "":
		return errors.New("the product name is needed")
	case len(b.Product) > maxFieldLen:
		return fmt.Errorf("the product name is longer than %d characters", maxFieldLen)
	case b.BatchNo == "":
		return errors.New("the batch number printed on the carton is needed")
	case len(b.BatchNo) > 40:
		return errors.New("that batch number is longer than 40 characters")
	case b.ExpiresOn.IsZero():
		return errors.New("the expiry date is needed — it is the whole point of the record")
	case b.Qty < 0:
		return errors.New("the quantity cannot be negative")
	case b.UnitCost < 0:
		return errors.New("the unit cost cannot be negative")
	case b.UnitPrice < 0:
		return errors.New("the unit price cannot be negative")
	case len(b.Pack) > maxFieldLen, len(b.Supplier) > maxFieldLen, len(b.Shelf) > maxFieldLen:
		return fmt.Errorf("one of the pack, supplier or shelf fields is longer than %d characters", maxFieldLen)
	case len(b.Note) > 500:
		return errors.New("the note is longer than 500 characters")
	case !b.PurchasedOn.IsZero() && b.PurchasedOn.After(b.ExpiresOn):
		return errors.New("the purchase date is after the expiry date — check which is which")
	}
	return nil
}

// Warnings lists what is odd about an otherwise valid batch: the things worth
// saying out loud without refusing to save the record.
func (b Batch) Warnings(p Policy) []string {
	var out []string
	if b.PurchasedOn.IsZero() && p.ApplyCreditNoteCutoff {
		out = append(out, "No purchase date, so the credit-note cut-off cannot be worked out for this batch.")
	}
	if b.UnitCost == 0 {
		out = append(out, "Unit cost is zero, so this batch contributes nothing to the money at risk.")
	}
	if b.UnitPrice > 0 && b.UnitPrice < b.UnitCost {
		out = append(out, "The unit price is below the unit cost — check which column is which.")
	}
	if !b.ExpiresOn.IsZero() && DaysBetween(Today(), b.ExpiresOn) > 3650 {
		out = append(out, "The expiry is more than ten years out, which usually means a mistyped year.")
	}
	return out
}

// Book is everything the tool stores: the shop's policy and its batches.
type Book struct {
	Policy  Policy  `json:"policy"`
	Batches []Batch `json:"batches"`
	// Seq is the last id handed out, kept in the file so an id is never reused
	// after a deletion.
	Seq int `json:"seq"`
}

// NewBook starts an empty book on the default policy.
func NewBook() *Book {
	return &Book{Policy: DefaultPolicy(), Batches: []Batch{}}
}

// Normalise repairs a book read off disk: policy defaults filled in, fields
// tidied, missing ids assigned and the sequence advanced past anything already
// in use.
func (bk *Book) Normalise() {
	bk.Policy.Normalise()
	if bk.Batches == nil {
		bk.Batches = []Batch{}
	}
	for i := range bk.Batches {
		bk.Batches[i].Normalise()
		if n := idNumber(bk.Batches[i].ID); n > bk.Seq {
			bk.Seq = n
		}
	}
	for i := range bk.Batches {
		if bk.Batches[i].ID == "" {
			bk.Seq++
			bk.Batches[i].ID = newID(bk.Seq)
		}
	}
}

func newID(n int) string { return "b" + strconv.Itoa(n) }

func idNumber(id string) int {
	if !strings.HasPrefix(id, "b") {
		return 0
	}
	n, err := strconv.Atoi(id[1:])
	if err != nil {
		return 0
	}
	return n
}

// Find returns a pointer to the batch with the given id, or nil.
func (bk *Book) Find(id string) *Batch {
	for i := range bk.Batches {
		if bk.Batches[i].ID == id {
			return &bk.Batches[i]
		}
	}
	return nil
}

// Upsert saves a batch: a new one when the id is empty, otherwise an edit of
// the batch with that id. It refuses a second record of the same product and
// batch number, because two rows for one lot is how a count goes wrong — the
// error points at the row that already exists so the owner can edit it instead.
func (bk *Book) Upsert(in Batch) (Batch, error) {
	in.Normalise()
	if err := in.Validate(); err != nil {
		return Batch{}, err
	}
	for i := range bk.Batches {
		if bk.Batches[i].ID != in.ID && bk.Batches[i].Key() == in.Key() {
			return Batch{}, fmt.Errorf("%s is already on file (batch %s of %s) — edit that row instead of adding a second one", bk.Batches[i].ID, bk.Batches[i].BatchNo, bk.Batches[i].Product)
		}
	}
	if in.ID == "" {
		bk.Seq++
		in.ID = newID(bk.Seq)
		bk.Batches = append(bk.Batches, in)
		return in, nil
	}
	existing := bk.Find(in.ID)
	if existing == nil {
		return Batch{}, ErrNotFound
	}
	*existing = in
	return in, nil
}

// Delete removes a batch outright. Use it for a row typed by mistake; use
// WriteOff for stock that really did expire, so the loss stays countable.
func (bk *Book) Delete(id string) error {
	for i := range bk.Batches {
		if bk.Batches[i].ID == id {
			bk.Batches = append(bk.Batches[:i], bk.Batches[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// WriteOff takes a batch off the shelf as a loss on the given day, keeping the
// quantity and the cost so the loss log can add it up later.
func (bk *Book) WriteOff(id string, on Date) error {
	b := bk.Find(id)
	if b == nil {
		return ErrNotFound
	}
	if b.WrittenOff {
		return errors.New("that batch is already written off")
	}
	if !b.ReturnedOn.IsZero() {
		return errors.New("that batch went back to the distributor, so it is not a loss")
	}
	if on.IsZero() {
		on = Today()
	}
	b.WrittenOff = true
	b.WrittenOffOn = on
	return nil
}

// MarkReturned records that the batch went back to the distributor on the given
// day, which stops it being nagged about and keeps it out of the loss log.
func (bk *Book) MarkReturned(id string, on Date) error {
	b := bk.Find(id)
	if b == nil {
		return ErrNotFound
	}
	if b.WrittenOff {
		return errors.New("that batch is written off as a loss; clear that first if it really went back")
	}
	if on.IsZero() {
		on = Today()
	}
	b.ReturnedOn = on
	return nil
}

// Reopen clears a write-off or a return, for the day somebody ticks the wrong
// row.
func (bk *Book) Reopen(id string) error {
	b := bk.Find(id)
	if b == nil {
		return ErrNotFound
	}
	b.WrittenOff = false
	b.WrittenOffOn = Date{}
	b.ReturnedOn = Date{}
	return nil
}

// Products lists the distinct product names in the book, sorted, for the
// dropdown on the pick list.
func (bk *Book) Products() []string {
	return distinct(bk.Batches, func(b Batch) string { return b.Product })
}

// Suppliers lists the distinct supplier names in the book, sorted.
func (bk *Book) Suppliers() []string {
	return distinct(bk.Batches, func(b Batch) string { return b.Supplier })
}

func distinct(batches []Batch, pick func(Batch) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range batches {
		v := pick(b)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	if out == nil {
		out = []string{}
	}
	return out
}

// squash trims the ends and collapses runs of whitespace, so a field pasted out
// of a spreadsheet does not carry a tab into a table cell.
func squash(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

// lowerKey is the comparison form of a name: squashed and lowercased, so
// "Amoxicillin  500" and "amoxicillin 500" are one product.
func lowerKey(s string) string { return strings.ToLower(squash(s)) }

// equalName compares two names the way a person would.
func equalName(a, b string) bool { return lowerKey(a) == lowerKey(b) }
