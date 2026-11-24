package stock

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// maxImportRows bounds an import so a wrong file cannot exhaust memory. A shop
// with more lines than this is not running on a spreadsheet any more.
const maxImportRows = 20000

// exportHeader is the column order of every CSV this tool writes. The first ten
// columns are exactly what the importer reads back, so an export can be edited
// in a spreadsheet and re-imported; the rest are worked out and are there for
// the person reading it.
var exportHeader = []string{
	"product", "pack", "batch_no", "expires_on", "purchased_on",
	"qty", "unit_cost", "unit_price", "supplier", "shelf",
	"tier", "days_left", "cost_value", "retail_value",
	"return_state", "return_opens_on", "return_deadline", "deadline_set_by", "recoverable",
	"status", "action", "note",
}

// ExportCSV writes the whole book, graded for the given day, as a CSV.
//
// Money is written plain — no currency sign and no digit grouping — so the
// columns add up in a spreadsheet instead of arriving as text.
func ExportCSV(w io.Writer, bk *Book, asOf Date) error {
	if asOf.IsZero() {
		asOf = Today()
	}
	out := csv.NewWriter(w)
	if err := out.Write(exportHeader); err != nil {
		return err
	}
	for _, b := range bk.Batches {
		row := GradeRow(b, asOf, bk.Policy)
		status := "on shelf"
		switch {
		case b.WrittenOff:
			status = "written off " + b.WrittenOffOn.String()
		case !b.ReturnedOn.IsZero():
			status = "returned " + b.ReturnedOn.String()
		}
		action := row.Action
		if b.Settled() {
			action = ""
		}
		record := []string{
			b.Product, b.Pack, b.BatchNo, b.ExpiresOn.String(), b.PurchasedOn.String(),
			strconv.Itoa(b.Qty), b.UnitCost.Plain(), b.UnitPrice.Plain(), b.Supplier, b.Shelf,
			string(row.Verdict.Tier), strconv.Itoa(daysLeftOrOverdue(row.Verdict)),
			row.CostValue.Plain(), row.RetailValue.Plain(),
			string(row.Verdict.Return.State), row.Verdict.Return.OpensOn.String(),
			row.Verdict.Return.Deadline.String(), row.Verdict.Return.LimitedBy, row.Recoverable.Plain(),
			status, action, b.Note,
		}
		if err := out.Write(record); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// daysLeftOrOverdue is signed: positive days remaining, negative once expired,
// so the column sorts the way a reader expects.
func daysLeftOrOverdue(v Verdict) int {
	if v.Tier == TierExpired {
		return -v.DaysExpired
	}
	return v.DaysLeft
}

// Problem is one line of an import that could not be read.
type Problem struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// ImportResult is what an import produced: the batches that were read, and a
// plain account of every line that was not.
type ImportResult struct {
	Batches   []Batch   `json:"batches"`
	Read      int       `json:"read"`
	Skipped   []Problem `json:"skipped"`
	Columns   []string  `json:"columns"`
	Duplicate int       `json:"duplicate"`
}

// aliases maps the column names a real spreadsheet uses onto the fields this
// tool needs. A shop's stock sheet says "item", "lot no", "exp", "qty", "rate"
// and "MRP"; refusing to read it because the header is not canonical would send
// the owner back to the spreadsheet for good.
var aliases = map[string][]string{
	"product":      {"product", "item", "item name", "product name", "description", "medicine", "drug", "particulars"},
	"pack":         {"pack", "packing", "unit", "pack size", "uom"},
	"batch_no":     {"batch_no", "batch", "batch no", "batchno", "batch number", "lot", "lot no", "lot number"},
	"expires_on":   {"expires_on", "expiry", "exp", "exp date", "expiry date", "expires", "exp dt", "expdate", "date of expiry"},
	"purchased_on": {"purchased_on", "purchase date", "purchased", "invoice date", "received", "received on", "bill date", "grn date"},
	"qty":          {"qty", "quantity", "stock", "balance", "on hand", "closing", "closing qty", "units"},
	"unit_cost":    {"unit_cost", "cost", "rate", "purchase rate", "purchase price", "ptr", "net rate", "cost price"},
	"unit_price":   {"unit_price", "mrp", "srp", "price", "retail", "retail price", "sale rate", "selling price"},
	"supplier":     {"supplier", "distributor", "vendor", "party", "supplier name", "dealer"},
	"shelf":        {"shelf", "rack", "location", "bin", "shelf no", "rack no"},
	"note":         {"note", "notes", "remark", "remarks", "comment"},
}

// resolveColumns reads the header row and reports which column holds which
// field, by index.
func resolveColumns(header []string) (map[string]int, []string) {
	lookup := map[string]string{}
	for field, names := range aliases {
		for _, n := range names {
			lookup[n] = field
		}
	}
	cols := map[string]int{}
	var matched []string
	for i, raw := range header {
		name := strings.ToLower(squash(strings.TrimPrefix(raw, "\xef\xbb\xbf")))
		name = strings.ReplaceAll(name, ".", "")
		field, ok := lookup[name]
		if !ok {
			continue
		}
		if _, already := cols[field]; already {
			continue
		}
		cols[field] = i
		matched = append(matched, field)
	}
	sort.Strings(matched)
	return cols, matched
}

// ImportCSV reads a stock sheet.
//
// It is forgiving about everything except the three things it cannot invent: a
// product name, a batch number and an expiry. A line missing any of those, or
// carrying a figure that is not a figure, is skipped and reported with its line
// number — nothing is silently guessed, because a guessed expiry is worse than
// no record at all.
//
// The expiry column is read with ParseExpiry, so "03/2027" and "MAR 2027" land
// on 31 March 2027 the way the carton means them.
func ImportCSV(r io.Reader) (ImportResult, error) {
	in := csv.NewReader(r)
	in.FieldsPerRecord = -1
	in.TrimLeadingSpace = true

	header, err := in.Read()
	if errors.Is(err, io.EOF) {
		return ImportResult{}, errors.New("the file is empty")
	}
	if err != nil {
		return ImportResult{}, fmt.Errorf("could not read the first line: %w", err)
	}
	cols, matched := resolveColumns(header)
	for _, need := range []string{"product", "batch_no", "expires_on"} {
		if _, ok := cols[need]; !ok {
			return ImportResult{}, fmt.Errorf("the file needs a %s column; the header read was: %s", strings.ReplaceAll(need, "_", " "), strings.Join(header, ", "))
		}
	}

	result := ImportResult{Batches: []Batch{}, Skipped: []Problem{}, Columns: matched}
	seen := map[string]int{}
	line := 1

	for {
		record, err := in.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line++
		if err != nil {
			result.Skipped = append(result.Skipped, Problem{Line: line, Message: "could not be read as CSV"})
			continue
		}
		if len(result.Batches) >= maxImportRows {
			return result, fmt.Errorf("stopped at %d rows; split the file and import it in parts", maxImportRows)
		}
		if blankRow(record) {
			continue
		}
		result.Read++

		b, err := batchFromRecord(record, cols)
		if err != nil {
			result.Skipped = append(result.Skipped, Problem{Line: line, Message: err.Error()})
			continue
		}
		if err := b.Validate(); err != nil {
			result.Skipped = append(result.Skipped, Problem{Line: line, Message: err.Error()})
			continue
		}
		if first, dup := seen[b.Key()]; dup {
			result.Duplicate++
			result.Skipped = append(result.Skipped, Problem{Line: line, Message: fmt.Sprintf("batch %s of %s is already on line %d", b.BatchNo, b.Product, first)})
			continue
		}
		seen[b.Key()] = line
		result.Batches = append(result.Batches, b)
	}
	return result, nil
}

func blankRow(record []string) bool {
	for _, f := range record {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

func batchFromRecord(record []string, cols map[string]int) (Batch, error) {
	get := func(field string) string {
		i, ok := cols[field]
		if !ok || i >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[i])
	}

	b := Batch{
		Product:  get("product"),
		Pack:     get("pack"),
		BatchNo:  get("batch_no"),
		Supplier: get("supplier"),
		Shelf:    get("shelf"),
		Note:     get("note"),
	}

	// The two text fields are checked before anything is parsed. A row with a
	// blank first cell has shifted every other column along, so reading the
	// expiry first would report an unreadable date when the real problem — and
	// the thing the owner has to fix — is the missing product name.
	b.Normalise()
	if b.Product == "" {
		return Batch{}, errors.New("the product name is needed")
	}
	if b.BatchNo == "" {
		return Batch{}, errors.New("the batch number is needed")
	}

	expiry := get("expires_on")
	if expiry == "" {
		return Batch{}, errors.New("no expiry date on this line")
	}
	expires, err := ParseExpiry(expiry)
	if err != nil {
		return Batch{}, fmt.Errorf("expiry %q could not be read", expiry)
	}
	b.ExpiresOn = expires

	if raw := get("purchased_on"); raw != "" {
		purchased, err := ParseDate(raw)
		if err != nil {
			return Batch{}, fmt.Errorf("purchase date %q could not be read", raw)
		}
		b.PurchasedOn = purchased
	}

	if raw := get("qty"); raw != "" {
		qty, err := strconv.Atoi(strings.ReplaceAll(strings.ReplaceAll(raw, ",", ""), " ", ""))
		if err != nil {
			return Batch{}, fmt.Errorf("quantity %q is not a whole number", raw)
		}
		b.Qty = qty
	}

	if raw := get("unit_cost"); raw != "" {
		cost, err := ParseMoney(raw)
		if err != nil {
			return Batch{}, fmt.Errorf("unit cost %q could not be read", raw)
		}
		b.UnitCost = cost
	}
	if raw := get("unit_price"); raw != "" {
		price, err := ParseMoney(raw)
		if err != nil {
			return Batch{}, fmt.Errorf("unit price %q could not be read", raw)
		}
		b.UnitPrice = price
	}

	b.Normalise()
	return b, nil
}
