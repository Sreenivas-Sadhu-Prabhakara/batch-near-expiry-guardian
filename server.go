package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/sreenivas-sadhu-prabhakara/batch-near-expiry-guardian/internal/stock"
)

// maxBodyBytes caps every request body. The interface never sends anything
// large, and a stock sheet of a few thousand lines is well inside this.
const maxBodyBytes = 8 << 20 // 8 MiB

// Server wires the local interface and the JSON API over one Store.
//
// Everything is same-origin: the interface is served out of the binary and
// talks to /api on the same loopback address. There is no other origin, no
// cookie, no account and no outbound request.
type Server struct {
	store *Store
	ui    fs.FS
	mux   *http.ServeMux
}

// NewServer builds the router.
func NewServer(store *Store, ui fs.FS) *Server {
	s := &Server{store: store, ui: ui, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A local tool still sets the headers that stop a stray page in another
	// tab from reading the shop's purchase prices.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.Handle("GET /", http.FileServerFS(s.ui))

	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("GET /api/board", s.handleBoard)
	s.mux.HandleFunc("GET /api/returns", s.handleReturns)
	s.mux.HandleFunc("GET /api/shelf", s.handleShelf)
	s.mux.HandleFunc("GET /api/pick", s.handlePick)
	s.mux.HandleFunc("GET /api/read-expiry", s.handleReadExpiry)
	s.mux.HandleFunc("GET /api/export.csv", s.handleExportCSV)

	s.mux.HandleFunc("PUT /api/batch", s.handlePutBatch)
	s.mux.HandleFunc("DELETE /api/batch", s.handleDeleteBatch)
	s.mux.HandleFunc("POST /api/batch/settle", s.handleSettle)
	s.mux.HandleFunc("POST /api/pick/apply", s.handleApplyPick)
	s.mux.HandleFunc("PUT /api/policy", s.handlePutPolicy)
	s.mux.HandleFunc("POST /api/import.csv", s.handleImportCSV)
}

// ───────────────────────── wire types ─────────────────────────

// money carries an amount both ways: the exact minor units for arithmetic and
// the formatted text for the screen, so the interface never does money maths
// and the two can never disagree.
type money struct {
	Minor int64  `json:"minor"`
	Text  string `json:"text"`
}

func cash(m stock.Money, c stock.Currency) money {
	return money{Minor: int64(m), Text: m.Format(c)}
}

type rowView struct {
	stock.Row
	Cost        money  `json:"cost"`
	Retail      money  `json:"retail"`
	Recoverable money  `json:"recoverable"`
	Status      string `json:"status"`
	Expiry      string `json:"expiry"`
	TierLabel   string `json:"tierLabel"`
	StateLabel  string `json:"stateLabel"`
	Says        string `json:"says"`
}

func view(row stock.Row, c stock.Currency) rowView {
	out := rowView{
		Row:         row,
		Cost:        cash(row.CostValue, c),
		Retail:      cash(row.RetailValue, c),
		Recoverable: cash(row.Recoverable, c),
		Expiry:      row.Batch.ExpiresOn.Pretty(),
		TierLabel:   row.Verdict.Tier.Label(),
		StateLabel:  row.Verdict.Return.State.Label(),
		Says:        row.Verdict.Describe(),
		Status:      "on shelf",
	}
	switch {
	case row.Batch.WrittenOff:
		out.Status = "written off " + row.Batch.WrittenOffOn.Pretty()
	case !row.Batch.ReturnedOn.IsZero():
		out.Status = "returned " + row.Batch.ReturnedOn.Pretty()
	}
	return out
}

func views(rows []stock.Row, c stock.Currency) []rowView {
	out := make([]rowView, 0, len(rows))
	for _, row := range rows {
		out = append(out, view(row, c))
	}
	return out
}

type bucketView struct {
	stock.Bucket
	Cost   money `json:"cost"`
	Retail money `json:"retail"`
}

type boardResponse struct {
	AsOf     string       `json:"asOf"`
	Currency string       `json:"currency"`
	Rows     []rowView    `json:"rows"`
	Buckets  []bucketView `json:"buckets"`
	Totals   struct {
		stock.Totals
		Cost   money `json:"cost"`
		Retail money `json:"retail"`
	} `json:"totals"`
	AtRisk  bucketView `json:"atRisk"`
	Returns struct {
		stock.Returns
		Open                money  `json:"open"`
		Recover             money  `json:"recover"`
		ClosingSoonRecover  money  `json:"closingSoonRecover"`
		NotYetOpen          money  `json:"notYetOpen"`
		Stranded            money  `json:"stranded"`
		Dead                money  `json:"dead"`
		NextDeadlinePretty  string `json:"nextDeadlinePretty"`
		CreditNoteLimitedBy int    `json:"creditNoteLimitedBy"`
	} `json:"returns"`
	Loss struct {
		stock.Loss
		Cost     money  `json:"cost"`
		Retail   money  `json:"retail"`
		Returned money  `json:"returned"`
		Share    string `json:"share"`
		SincePr  string `json:"sincePretty"`
	} `json:"loss"`
}

func boardView(b stock.Board) boardResponse {
	c := b.Currency
	out := boardResponse{
		AsOf:     b.AsOf.String(),
		Currency: string(c),
		Rows:     views(b.Rows, c),
		Buckets:  make([]bucketView, 0, len(b.Buckets)),
	}
	for _, bucket := range b.Buckets {
		out.Buckets = append(out.Buckets, bucketView{Bucket: bucket, Cost: cash(bucket.CostValue, c), Retail: cash(bucket.RetailValue, c)})
	}
	out.AtRisk = bucketView{Bucket: b.AtRisk, Cost: cash(b.AtRisk.CostValue, c), Retail: cash(b.AtRisk.RetailValue, c)}

	out.Totals.Totals = b.Totals
	out.Totals.Cost = cash(b.Totals.CostValue, c)
	out.Totals.Retail = cash(b.Totals.RetailValue, c)

	out.Returns.Returns = b.Returns
	out.Returns.Open = cash(b.Returns.OpenCostValue, c)
	out.Returns.Recover = cash(b.Returns.Recoverable, c)
	out.Returns.ClosingSoonRecover = cash(b.Returns.ClosingSoonRecoverable, c)
	out.Returns.NotYetOpen = cash(b.Returns.NotYetOpenCostValue, c)
	out.Returns.Stranded = cash(b.Returns.StrandedCostValue, c)
	out.Returns.Dead = cash(b.Returns.DeadCostValue, c)
	out.Returns.NextDeadlinePretty = b.Returns.NextDeadline.Pretty()
	out.Returns.CreditNoteLimitedBy = b.Returns.CreditNoteLimited

	out.Loss.Loss = b.Loss
	out.Loss.Cost = cash(b.Loss.CostValue, c)
	out.Loss.Retail = cash(b.Loss.RetailValue, c)
	out.Loss.Returned = cash(b.Loss.ReturnedCostValue, c)
	out.Loss.Share = sharePercent(b.Loss.SharePerMille)
	out.Loss.SincePr = b.Loss.Since.Pretty()
	return out
}

// sharePercent turns tenths of a percent into the string a person reads.
func sharePercent(perMille int) string {
	sign := ""
	if perMille < 0 {
		sign, perMille = "-", -perMille
	}
	return fmt.Sprintf("%s%d.%d%%", sign, perMille/10, perMille%10)
}

// ───────────────────────── reads ─────────────────────────

type defaultsView struct {
	WatchDays        int `json:"watchDays"`
	WarnDays         int `json:"warnDays"`
	UrgentDays       int `json:"urgentDays"`
	ReturnOpensDays  int `json:"returnOpensDays"`
	ReturnClosesDays int `json:"returnClosesDays"`
	ReturnLeadDays   int `json:"returnLeadDays"`
	CreditPercent    int `json:"creditPercent"`
	ClosingSoonDays  int `json:"closingSoonDays"`
	LossWindowDays   int `json:"lossWindowDays"`
}

type stateResponse struct {
	Today       string              `json:"today"`
	DataFile    string              `json:"dataFile"`
	Policy      stock.Policy        `json:"policy"`
	Market      string              `json:"market"`
	Currency    string              `json:"currency"`
	Symbol      string              `json:"symbol"`
	Batches     []stock.Batch       `json:"batches"`
	Products    []string            `json:"products"`
	Suppliers   []string            `json:"suppliers"`
	Authorities []stock.Authority   `json:"authorities"`
	Defaults    defaultsView        `json:"defaults"`
	ExpiryNote  string              `json:"expiryNote"`
	Markets     []map[string]string `json:"markets"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	out := stateResponse{
		Today:       stock.Today().String(),
		DataFile:    s.store.Path(),
		Authorities: stock.Authorities(),
		ExpiryNote:  stock.MonthEndExpiry,
		Defaults: defaultsView{
			WatchDays:        stock.DefaultWatchDays,
			WarnDays:         stock.DefaultWarnDays,
			UrgentDays:       stock.DefaultUrgentDays,
			ReturnOpensDays:  stock.DefaultReturnOpensDays,
			ReturnClosesDays: stock.DefaultReturnClosesDays,
			ReturnLeadDays:   stock.DefaultReturnLeadDays,
			CreditPercent:    stock.DefaultCreditPercent,
			ClosingSoonDays:  stock.DefaultClosingSoonDays,
			LossWindowDays:   stock.DefaultLossWindowDays,
		},
		Markets: []map[string]string{
			{"code": string(stock.India), "label": stock.India.Label()},
			{"code": string(stock.Philippines), "label": stock.Philippines.Label()},
		},
	}
	s.store.Read(func(bk *stock.Book) {
		out.Policy = bk.Policy
		out.Market = bk.Policy.Market.Label()
		out.Currency = string(bk.Policy.Market.Currency())
		out.Symbol = bk.Policy.Market.Currency().Symbol()
		out.Batches = append(out.Batches, bk.Batches...)
		out.Products = bk.Products()
		out.Suppliers = bk.Suppliers()
	})
	if out.Batches == nil {
		out.Batches = []stock.Batch{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	asOf, err := asOfParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var out boardResponse
	s.store.Read(func(bk *stock.Book) { out = boardView(stock.NewBoard(bk, asOf)) })
	writeJSON(w, http.StatusOK, out)
}

type returnsResponse struct {
	AsOf    string    `json:"asOf"`
	Rows    []rowView `json:"rows"`
	Total   money     `json:"total"`
	Recover money     `json:"recover"`
	Note    string    `json:"note"`
}

func (s *Server) handleReturns(w http.ResponseWriter, r *http.Request) {
	asOf, err := asOfParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	out := returnsResponse{Rows: []rowView{}}
	s.store.Read(func(bk *stock.Book) {
		c := bk.Policy.Market.Currency()
		rows := stock.ReturnList(bk, asOf)
		var total, recover stock.Money
		for _, row := range rows {
			total += row.CostValue
			recover += row.Recoverable
		}
		out.AsOf = asOfOrToday(asOf).String()
		out.Rows = views(rows, c)
		out.Total = cash(total, c)
		out.Recover = cash(recover, c)
		out.Note = fmt.Sprintf("Credit worked out at %d%% of what the stock cost, which is the figure in Settings and not a figure any regulator sets.", bk.Policy.CreditPercent)
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleShelf(w http.ResponseWriter, r *http.Request) {
	asOf, err := asOfParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	product := strings.TrimSpace(r.URL.Query().Get("product"))
	if product == "" {
		writeError(w, http.StatusBadRequest, errors.New("choose a product first"))
		return
	}
	out := struct {
		Product string    `json:"product"`
		AsOf    string    `json:"asOf"`
		Rows    []rowView `json:"rows"`
	}{Product: product, AsOf: asOfOrToday(asOf).String(), Rows: []rowView{}}
	s.store.Read(func(bk *stock.Book) {
		out.Rows = views(stock.ShelfOrder(bk, product, asOf), bk.Policy.Market.Currency())
	})
	writeJSON(w, http.StatusOK, out)
}

type pickResponse struct {
	stock.PickList
	Value money  `json:"value"`
	Note  string `json:"note"`
}

func (s *Server) handlePick(w http.ResponseWriter, r *http.Request) {
	asOf, err := asOfParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	qty, err := intParam(r, "qty")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	var out pickResponse
	var fail error
	s.store.Read(func(bk *stock.Book) {
		list, err := stock.Pick(bk.Batches, r.URL.Query().Get("product"), qty, asOf, bk.Policy)
		if err != nil {
			fail = err
			return
		}
		var value stock.Money
		for _, line := range list.Lines {
			value += line.Value
		}
		out = pickResponse{
			PickList: list,
			Value:    cash(value, bk.Policy.Market.Currency()),
			Note:     "First expiry, first out. Nothing is taken off the shelf until you confirm.",
		}
	})
	if fail != nil {
		writeError(w, http.StatusBadRequest, fail)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleReadExpiry shows what the tool makes of an expiry as it is typed, so a
// shop can see that "03/2027" became 31 March 2027 before saving the batch.
func (s *Server) handleReadExpiry(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("v")
	parsed, err := stock.ParseExpiry(raw)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"iso":    parsed.String(),
		"pretty": parsed.Pretty(),
		"month":  parsed.MonthLabel(),
		"note":   stock.MonthEndExpiry,
	})
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	asOf, err := asOfParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="batches.csv"`)
	var fail error
	s.store.Read(func(bk *stock.Book) { fail = stock.ExportCSV(w, bk, asOf) })
	if fail != nil {
		// The header is already out, so the best that can be done is to stop
		// writing and leave a visible marker in the file.
		fmt.Fprintf(w, "\n# the export stopped early: %v\n", fail)
	}
}

// ───────────────────────── writes ─────────────────────────

// batchInput is a batch as the interface sends it: the expiry and the money
// arrive as the text that was typed, so the domain's own readers decide what
// "03/2027" and "1,210.50" mean rather than a second parser in the browser.
type batchInput struct {
	ID          string `json:"id"`
	Product     string `json:"product"`
	Pack        string `json:"pack"`
	BatchNo     string `json:"batchNo"`
	ExpiresOn   string `json:"expiresOn"`
	PurchasedOn string `json:"purchasedOn"`
	Qty         int    `json:"qty"`
	UnitCost    string `json:"unitCost"`
	UnitPrice   string `json:"unitPrice"`
	Supplier    string `json:"supplier"`
	Shelf       string `json:"shelf"`
	Note        string `json:"note"`
}

func (in batchInput) batch() (stock.Batch, error) {
	b := stock.Batch{
		ID: strings.TrimSpace(in.ID), Product: in.Product, Pack: in.Pack, BatchNo: in.BatchNo,
		Qty: in.Qty, Supplier: in.Supplier, Shelf: in.Shelf, Note: in.Note,
	}
	expires, err := stock.ParseExpiry(in.ExpiresOn)
	if err != nil {
		if errors.Is(err, stock.ErrEmptyDate) {
			return stock.Batch{}, errors.New("the expiry printed on the pack is needed")
		}
		return stock.Batch{}, err
	}
	b.ExpiresOn = expires

	if strings.TrimSpace(in.PurchasedOn) != "" {
		purchased, err := stock.ParseDate(in.PurchasedOn)
		if err != nil {
			return stock.Batch{}, err
		}
		b.PurchasedOn = purchased
	}
	if b.UnitCost, err = optionalMoney(in.UnitCost, "unit cost"); err != nil {
		return stock.Batch{}, err
	}
	if b.UnitPrice, err = optionalMoney(in.UnitPrice, "unit price"); err != nil {
		return stock.Batch{}, err
	}
	return b, nil
}

func optionalMoney(raw, what string) (stock.Money, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	m, err := stock.ParseMoney(raw)
	if err != nil {
		return 0, fmt.Errorf("the %s could not be read: %w", what, err)
	}
	return m, nil
}

func (s *Server) handlePutBatch(w http.ResponseWriter, r *http.Request) {
	var in batchInput
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	b, err := in.batch()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var saved stock.Batch
	var warnings []string
	if err := s.store.Write(func(bk *stock.Book) error {
		out, err := bk.Upsert(b)
		if err != nil {
			return err
		}
		saved = out
		warnings = out.Warnings(bk.Policy)
		return nil
	}); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": saved, "warnings": warnings})
}

func (s *Server) handleDeleteBatch(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("which batch?"))
		return
	}
	if err := s.store.Write(func(bk *stock.Book) error { return bk.Delete(id) }); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

// handleSettle records what happened to a batch: it went back to the
// distributor, it was written off as a loss, or neither after all.
func (s *Server) handleSettle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     string `json:"id"`
		Action string `json:"action"`
		On     string `json:"on"`
	}
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.ID = strings.TrimSpace(in.ID)
	if in.ID == "" {
		writeError(w, http.StatusBadRequest, errors.New("which batch?"))
		return
	}
	var on stock.Date
	if strings.TrimSpace(in.On) != "" {
		parsed, err := stock.ParseDate(in.On)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		on = parsed
	}

	err := s.store.Write(func(bk *stock.Book) error {
		switch in.Action {
		case "returned":
			return bk.MarkReturned(in.ID, on)
		case "writeOff":
			return bk.WriteOff(in.ID, on)
		case "reopen":
			return bk.Reopen(in.ID)
		default:
			return fmt.Errorf("unknown action %q", in.Action)
		}
	})
	if err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": in.ID, "action": in.Action})
}

// handleApplyPick takes a pick list off the shelf.
//
// The list is rebuilt from the book inside the write lock rather than trusted
// from the browser, so what is deducted is exactly what FEFO says to deduct on
// the stock that is actually there at that moment.
func (s *Server) handleApplyPick(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Product string `json:"product"`
		Qty     int    `json:"qty"`
		AsOf    string `json:"asOf"`
	}
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var asOf stock.Date
	if strings.TrimSpace(in.AsOf) != "" {
		parsed, err := stock.ParseDate(in.AsOf)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		asOf = parsed
	}

	var applied stock.PickList
	if err := s.store.Write(func(bk *stock.Book) error {
		list, err := stock.Pick(bk.Batches, in.Product, in.Qty, asOf, bk.Policy)
		if err != nil {
			return err
		}
		if err := bk.ApplyPick(list); err != nil {
			return err
		}
		applied = list
		return nil
	}); err != nil {
		writeError(w, statusFor(err), err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied})
}

func (s *Server) handlePutPolicy(w http.ResponseWriter, r *http.Request) {
	var in stock.Policy
	if err := readJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if !in.Market.Valid() {
		writeError(w, http.StatusBadRequest, errors.New("choose India or the Philippines"))
		return
	}
	if in.Market != stock.India {
		in.ApplyCreditNoteCutoff = false
	}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.Write(func(bk *stock.Book) error {
		bk.Policy = in
		return nil
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": in})
}

// handleImportCSV replaces the stock list with the one in the file.
//
// It replaces rather than merges, and says so, because merging two counts of
// one shelf is how a count silently doubles. The shop's policy is kept: a
// stock sheet says nothing about return terms.
func (s *Server) handleImportCSV(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer body.Close()

	result, err := stock.ImportCSV(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(result.Batches) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("nothing in that file could be read as a batch, so the stock list has been left alone"))
		return
	}
	if err := s.store.ReplaceBatches(result.Batches); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"imported": len(result.Batches),
		"read":     result.Read,
		"skipped":  result.Skipped,
		"columns":  result.Columns,
		"replaced": true,
	})
}

// ───────────────────────── plumbing ─────────────────────────

func asOfParam(r *http.Request) (stock.Date, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("asOf"))
	if raw == "" {
		return stock.Date{}, nil
	}
	return stock.ParseDate(raw)
}

func asOfOrToday(d stock.Date) stock.Date {
	if d.IsZero() {
		return stock.Today()
	}
	return d
}

func intParam(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, fmt.Errorf("%s is needed", name)
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number", name)
	}
	return n, nil
}

// statusFor maps a domain error onto an HTTP status. Everything the domain
// rejects is the caller's fault except a missing record, which is a 404.
func statusFor(err error) int {
	if errors.Is(err, stock.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func readJSON(w http.ResponseWriter, r *http.Request, into any) error {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("the request could not be read: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("the request carried more than one object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line has gone, so there is nothing useful left to say to
		// the caller; the browser will show a broken response and the owner
		// will reload.
		return
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
