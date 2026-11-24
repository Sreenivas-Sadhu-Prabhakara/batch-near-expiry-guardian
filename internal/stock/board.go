package stock

import "sort"

// Row is one batch on the board, graded and priced.
type Row struct {
	Batch   Batch   `json:"batch"`
	Verdict Verdict `json:"verdict"`
	// CostValue is what the units on hand cost to buy.
	CostValue Money `json:"costValue"`
	// RetailValue is what they would have sold for.
	RetailValue Money `json:"retailValue"`
	// Recoverable is the credit you would expect if this batch went back today:
	// the cost value at the credit rate in Settings, and zero whenever the
	// return window is not open, because an expected credit you cannot claim is
	// not money.
	Recoverable Money `json:"recoverable"`
	// Action is the single next thing to do with this batch.
	Action string `json:"action"`
	// Describe is the batch in one line, so a table does not rebuild it.
	Describe string `json:"describe"`
}

// Bucket is the count and the value of one tier.
type Bucket struct {
	Tier        Tier   `json:"tier"`
	Label       string `json:"label"`
	Band        string `json:"band"`
	Batches     int    `json:"batches"`
	Units       int    `json:"units"`
	CostValue   Money  `json:"costValue"`
	RetailValue Money  `json:"retailValue"`
}

// Totals is the whole of the live stock on hand.
type Totals struct {
	Batches     int   `json:"batches"`
	Units       int   `json:"units"`
	CostValue   Money `json:"costValue"`
	RetailValue Money `json:"retailValue"`
	Products    int   `json:"products"`
}

// Returns is the money question, split so that no figure overstates the case.
//
// Recoverable is money you can actually get back, today, by sending stock to
// the distributor. Stranded stock has not been lost — it can still be sold —
// but it can no longer be returned, so the only way out of it is over the
// counter. Dead is the loss that has already happened.
type Returns struct {
	// OpenBatches and OpenCostValue cover every batch the distributor would
	// take back today.
	OpenBatches   int   `json:"openBatches"`
	OpenCostValue Money `json:"openCostValue"`
	// Recoverable is the credit expected on those batches.
	Recoverable Money `json:"recoverable"`
	// ClosingSoon is the part of that inside the closing-soon notice period.
	ClosingSoonBatches     int   `json:"closingSoonBatches"`
	ClosingSoonRecoverable Money `json:"closingSoonRecoverable"`
	// NextDeadline is the earliest return deadline still ahead, which is the
	// one date worth putting in a diary.
	NextDeadline Date `json:"nextDeadline"`
	// CreditNoteLimited counts the open batches whose deadline is set by the
	// statutory credit-note cut-off rather than the distributor's window.
	CreditNoteLimited int `json:"creditNoteLimited"`
	// NotYetOpen is stock the distributor will not take back yet.
	NotYetOpenBatches   int   `json:"notYetOpenBatches"`
	NotYetOpenCostValue Money `json:"notYetOpenCostValue"`
	// Stranded is short-dated stock past its return window but still saleable:
	// sell it down or it becomes the loss below.
	StrandedBatches   int   `json:"strandedBatches"`
	StrandedCostValue Money `json:"strandedCostValue"`
	// Dead is expired stock on hand: cannot be sold, cannot be returned.
	DeadBatches   int   `json:"deadBatches"`
	DeadCostValue Money `json:"deadCostValue"`
	DeadUnits     int   `json:"deadUnits"`
}

// Loss is what expiry has already cost, over the look-back window in Settings.
//
// This is the number the shop has never been able to see. The trade rule of
// thumb is that an independent pharmacy loses a few percent of stock value to
// expiry; a book of written-off batches turns that rule of thumb into the
// shop's own figure.
type Loss struct {
	WindowDays  int   `json:"windowDays"`
	Since       Date  `json:"since"`
	Batches     int   `json:"batches"`
	Units       int   `json:"units"`
	CostValue   Money `json:"costValue"`
	RetailValue Money `json:"retailValue"`
	// SharePerMille is the written-off cost value as tenths of a percent of the
	// cost value of stock on hand today. It is a ratio against today's holding,
	// not against a year of purchases, because this tool does not see
	// purchases — read it as an order of magnitude, not an audited figure.
	SharePerMille int `json:"sharePerMille"`
	// Returned counts batches that went back to the distributor in the window
	// instead of dying on the shelf: the tool's own scoreboard.
	ReturnedBatches   int   `json:"returnedBatches"`
	ReturnedCostValue Money `json:"returnedCostValue"`
}

// Board is the whole dashboard for one day.
type Board struct {
	AsOf     Date     `json:"asOf"`
	Currency Currency `json:"currency"`
	Rows     []Row    `json:"rows"`
	Buckets  []Bucket `json:"buckets"`
	Totals   Totals   `json:"totals"`
	// AtRisk is the stock inside one of the alert tiers but not yet expired:
	// the value the shop can still do something about today. It is the first
	// number on the screen, because it is the one that makes an owner act.
	AtRisk  Bucket  `json:"atRisk"`
	Returns Returns `json:"returns"`
	Loss    Loss    `json:"loss"`
}

// tierOrder is the order buckets are reported in, worst first.
func tierOrder() []Tier {
	return []Tier{TierExpired, TierUrgent, TierWarn, TierWatch, TierFresh, TierUnknown}
}

// GradeRow prices and grades one batch.
func GradeRow(b Batch, asOf Date, p Policy) Row {
	v := Grade(b.ExpiresOn, b.PurchasedOn, asOf, p)
	row := Row{
		Batch:       b,
		Verdict:     v,
		CostValue:   b.CostValue(),
		RetailValue: b.RetailValue(),
		Action:      v.Action(b.Supplier),
		Describe:    b.Describe(),
	}
	if v.Return.Open() && v.Tier.Dispensable() {
		row.Recoverable = Percent(row.CostValue, p.CreditPercent)
	}
	return row
}

// NewBoard grades every live batch in the book for the given day and adds up
// the money.
//
// Batches already returned or written off are not on the board — they are not
// on the shelf — but they are counted in the loss log, which is the only place
// a shop can see what expiry is costing it.
func NewBoard(bk *Book, asOf Date) Board {
	if asOf.IsZero() {
		asOf = Today()
	}
	p := bk.Policy
	board := Board{
		AsOf:     asOf,
		Currency: p.Market.Currency(),
		Rows:     []Row{},
		AtRisk:   Bucket{Label: "About to expire", Band: BandAmber},
	}

	byTier := map[Tier]*Bucket{}
	products := map[string]bool{}

	for _, b := range bk.Batches {
		if b.Settled() {
			continue
		}
		row := GradeRow(b, asOf, p)
		board.Rows = append(board.Rows, row)

		board.Totals.Batches++
		board.Totals.Units += b.Qty
		board.Totals.CostValue += row.CostValue
		board.Totals.RetailValue += row.RetailValue
		products[lowerKey(b.Product)] = true

		bucket := byTier[row.Verdict.Tier]
		if bucket == nil {
			bucket = &Bucket{Tier: row.Verdict.Tier, Label: row.Verdict.Tier.Label(), Band: row.Verdict.Tier.Band()}
			byTier[row.Verdict.Tier] = bucket
		}
		bucket.Batches++
		bucket.Units += b.Qty
		bucket.CostValue += row.CostValue
		bucket.RetailValue += row.RetailValue

		// Fresh stock is not at risk and expired stock is past risk: it is a
		// loss. What is left is what is worth a decision this week.
		switch row.Verdict.Tier {
		case TierWatch, TierWarn, TierUrgent:
			board.AtRisk.Batches++
			board.AtRisk.Units += b.Qty
			board.AtRisk.CostValue += row.CostValue
			board.AtRisk.RetailValue += row.RetailValue
		}

		accrueReturns(&board.Returns, row, p)
	}
	board.Totals.Products = len(products)

	for _, t := range tierOrder() {
		if bucket := byTier[t]; bucket != nil {
			board.Buckets = append(board.Buckets, *bucket)
		}
	}
	if board.Buckets == nil {
		board.Buckets = []Bucket{}
	}

	sortRows(board.Rows)
	board.Loss = NewLoss(bk, asOf, board.Totals.CostValue)
	return board
}

// accrueReturns folds one row into the returns summary.
func accrueReturns(r *Returns, row Row, p Policy) {
	if row.Verdict.Tier == TierExpired {
		r.DeadBatches++
		r.DeadUnits += row.Batch.Qty
		r.DeadCostValue += row.CostValue
		return
	}
	switch row.Verdict.Return.State {
	case ReturnOpen:
		r.OpenBatches++
		r.OpenCostValue += row.CostValue
		r.Recoverable += row.Recoverable
		if row.Verdict.Return.LimitedBy == LimitByCreditNote {
			r.CreditNoteLimited++
		}
		if row.Verdict.Return.DaysToDeadline <= p.ClosingSoonDays {
			r.ClosingSoonBatches++
			r.ClosingSoonRecoverable += row.Recoverable
		}
		if r.NextDeadline.IsZero() || row.Verdict.Return.Deadline.Before(r.NextDeadline) {
			r.NextDeadline = row.Verdict.Return.Deadline
		}
	case ReturnTooEarly:
		r.NotYetOpenBatches++
		r.NotYetOpenCostValue += row.CostValue
	default:
		r.StrandedBatches++
		r.StrandedCostValue += row.CostValue
	}
}

// NewLoss adds up what has already been written off, and what was rescued, in
// the look-back window ending on asOf.
func NewLoss(bk *Book, asOf Date, stockCostValue Money) Loss {
	if asOf.IsZero() {
		asOf = Today()
	}
	window := bk.Policy.LossWindowDays
	if window <= 0 {
		window = DefaultLossWindowDays
	}
	loss := Loss{WindowDays: window, Since: asOf.AddDays(-window + 1)}

	for _, b := range bk.Batches {
		switch {
		case b.WrittenOff:
			if inWindow(b.WrittenOffOn, loss.Since, asOf) {
				loss.Batches++
				loss.Units += b.Qty
				loss.CostValue += b.CostValue()
				loss.RetailValue += b.RetailValue()
			}
		case !b.ReturnedOn.IsZero():
			if inWindow(b.ReturnedOn, loss.Since, asOf) {
				loss.ReturnedBatches++
				loss.ReturnedCostValue += b.CostValue()
			}
		}
	}
	if stockCostValue > 0 {
		loss.SharePerMille = int(int64(loss.CostValue) * 1000 / int64(stockCostValue))
	}
	return loss
}

func inWindow(d, since, until Date) bool {
	if d.IsZero() {
		return false
	}
	return !d.Before(since) && !d.After(until)
}

// sortRows puts the board in the order a person works down it: the worst tier
// first, the batch that dies soonest first inside a tier, and the biggest money
// first where two batches die on the same day.
func sortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if ra, rb := a.Verdict.Tier.Rank(), b.Verdict.Tier.Rank(); ra != rb {
			return ra < rb
		}
		if a.Batch.ExpiresOn != b.Batch.ExpiresOn {
			return a.Batch.ExpiresOn.Before(b.Batch.ExpiresOn)
		}
		if a.CostValue != b.CostValue {
			return a.CostValue > b.CostValue
		}
		return lowerKey(a.Batch.Product) < lowerKey(b.Batch.Product)
	})
}

// ReturnList is the returns worksheet: every live batch the distributor would
// take back now or soon, with the deadline that bites, earliest first.
//
// Stock whose window has not opened is included only when it opens inside the
// closing-soon notice period, so the list stays short enough to work through
// in one sitting. Expired stock is never here — it cannot go back.
func ReturnList(bk *Book, asOf Date) []Row {
	if asOf.IsZero() {
		asOf = Today()
	}
	p := bk.Policy
	out := []Row{}
	for _, b := range bk.Batches {
		if b.Settled() || b.Qty <= 0 {
			continue
		}
		row := GradeRow(b, asOf, p)
		if row.Verdict.Tier == TierExpired || row.Verdict.Tier == TierUnknown {
			continue
		}
		switch row.Verdict.Return.State {
		case ReturnOpen:
		case ReturnTooEarly:
			if DaysBetween(asOf, row.Verdict.Return.OpensOn) > p.ClosingSoonDays {
				continue
			}
		default:
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Verdict.Return.Deadline != b.Verdict.Return.Deadline {
			return a.Verdict.Return.Deadline.Before(b.Verdict.Return.Deadline)
		}
		if a.Recoverable != b.Recoverable {
			return a.Recoverable > b.Recoverable
		}
		return lowerKey(a.Batch.Product) < lowerKey(b.Batch.Product)
	})
	return out
}

// ShelfOrder is the FEFO shelf list for one product: every live batch in the
// order it should be pulled, so staff can physically rearrange the shelf.
func ShelfOrder(bk *Book, product string, asOf Date) []Row {
	if asOf.IsZero() {
		asOf = Today()
	}
	out := []Row{}
	for _, b := range FEFOOrder(bk.Batches) {
		if b.Settled() || !equalName(b.Product, product) {
			continue
		}
		out = append(out, GradeRow(b, asOf, bk.Policy))
	}
	return out
}
