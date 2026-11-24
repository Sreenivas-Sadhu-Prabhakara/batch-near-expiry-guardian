package main

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sreenivas-sadhu-prabhakara/batch-near-expiry-guardian/internal/stock"
)

// newTestServer builds a server over a fresh data file and the real embedded
// interface, so the tests also prove the interface ships inside the binary.
func newTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	store, err := OpenStore(tempData(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	ui, err := fs.Sub(webFiles, "web")
	if err != nil {
		t.Fatalf("the interface is not embedded: %v", err)
	}
	return NewServer(store, ui), store
}

func do(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("response is not JSON (%d): %s", rec.Code, rec.Body.String())
	}
}

func mustPost(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := do(t, s, method, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d: %s", method, path, rec.Code, rec.Body.String())
	}
	return rec
}

// seed puts one well-known batch on the shelf and returns its id.
func seed(t *testing.T, s *Server) string {
	t.Helper()
	rec := mustPost(t, s, "PUT", "/api/batch", `{
		"product":"Amoxicillin 500 mg capsule","pack":"strip of 10","batchNo":"ax2291",
		"expiresOn":"06/2027","purchasedOn":"01-08-2026","qty":180,
		"unitCost":"12.10","unitPrice":"18.00","supplier":"Metro Distributors","shelf":"A3","note":""}`)
	var out struct {
		Batch    stock.Batch `json:"batch"`
		Warnings []string    `json:"warnings"`
	}
	decode(t, rec, &out)
	if out.Batch.ID == "" {
		t.Fatalf("no id came back: %s", rec.Body.String())
	}
	if out.Batch.BatchNo != "AX2291" {
		t.Errorf("batch number = %q, want it uppercased", out.Batch.BatchNo)
	}
	// "06/2027" is a month-only expiry, so it lands on the last day of June.
	if out.Batch.ExpiresOn != stock.NewDate(2027, time.June, 30) {
		t.Errorf("expiry = %s, want 2027-06-30", out.Batch.ExpiresOn)
	}
	if out.Batch.UnitCost != 1210 {
		t.Errorf("unit cost = %d, want 1210", out.Batch.UnitCost)
	}
	return out.Batch.ID
}

func TestSecurityHeadersAndEmbeddedInterface(t *testing.T) {
	s, _ := newTestServer(t)
	rec := do(t, s, "GET", "/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<h1>Medicine batch and expiry tracker for independent pharmacies</h1>") {
		t.Error("the embedded interface does not carry the expected heading")
	}
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"Cache-Control":          "no-store",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	for _, asset := range []string{"/assets/app.js", "/assets/app.css", "/assets/style.css", "/assets/accent.css", "/assets/theme-init.js", "/assets/favicon.svg"} {
		if got := do(t, s, "GET", asset, "").Code; got != http.StatusOK {
			t.Errorf("GET %s = %d; the interface must be complete inside the binary", asset, got)
		}
	}
}

func TestStateEndpoint(t *testing.T) {
	s, store := newTestServer(t)
	seed(t, s)

	rec := mustPost(t, s, "GET", "/api/state", "")
	var out stateResponse
	decode(t, rec, &out)

	if out.Today != stock.Today().String() {
		t.Errorf("today = %q", out.Today)
	}
	if out.DataFile != store.Path() {
		t.Errorf("dataFile = %q, want %q", out.DataFile, store.Path())
	}
	if out.Currency != "INR" || out.Symbol != "₹" {
		t.Errorf("currency = %q / %q", out.Currency, out.Symbol)
	}
	if len(out.Batches) != 1 {
		t.Fatalf("got %d batches", len(out.Batches))
	}
	if len(out.Products) != 1 || out.Products[0] != "Amoxicillin 500 mg capsule" {
		t.Errorf("products = %v", out.Products)
	}
	if len(out.Suppliers) != 1 {
		t.Errorf("suppliers = %v", out.Suppliers)
	}
	if len(out.Authorities) < 3 {
		t.Errorf("only %d authorities sent; the interface has to be able to show where the rules come from", len(out.Authorities))
	}
	if out.Defaults.WatchDays != stock.DefaultWatchDays || out.Defaults.ReturnClosesDays != stock.DefaultReturnClosesDays {
		t.Errorf("defaults = %+v, want the named constants", out.Defaults)
	}
	if out.ExpiryNote == "" {
		t.Error("the month-end expiry convention was not sent")
	}
	if len(out.Markets) != 2 {
		t.Errorf("markets = %v", out.Markets)
	}
}

func TestBoardEndpointQuantifiesTheMoney(t *testing.T) {
	s, _ := newTestServer(t)
	seed(t, s)

	// On 1 March 2027 the batch has 121 days left (fresh) and its window is
	// open: 180 days before 30 June 2027 is 1 January 2027, and it closes on
	// 1 April 2027.
	rec := mustPost(t, s, "GET", "/api/board?asOf=2027-03-01", "")
	var out boardResponse
	decode(t, rec, &out)

	if out.AsOf != "2027-03-01" {
		t.Errorf("asOf = %q", out.AsOf)
	}
	if out.Currency != "INR" {
		t.Errorf("currency = %q", out.Currency)
	}
	if len(out.Rows) != 1 {
		t.Fatalf("got %d rows", len(out.Rows))
	}

	row := out.Rows[0]
	// 180 units at 12.10 = 2,178.00 at cost; at 18.00 = 3,240.00 at retail.
	if row.Cost.Minor != 217800 || row.Cost.Text != "₹2,178.00" {
		t.Errorf("cost = %d / %q", row.Cost.Minor, row.Cost.Text)
	}
	if row.Retail.Minor != 324000 {
		t.Errorf("retail = %d", row.Retail.Minor)
	}
	if row.Recoverable.Minor != 217800 {
		t.Errorf("recoverable = %d, want the full cost at the default 100%% credit rate", row.Recoverable.Minor)
	}
	if row.Verdict.Return.State != stock.ReturnOpen {
		t.Errorf("return state = %s", row.Verdict.Return.State)
	}
	if row.Verdict.Return.Deadline != stock.NewDate(2027, time.April, 1) {
		t.Errorf("deadline = %s, want 2027-04-01", row.Verdict.Return.Deadline)
	}
	if !strings.Contains(row.Action, "Return to Metro Distributors by 01-04-2027") {
		t.Errorf("action = %q", row.Action)
	}
	if row.Status != "on shelf" || row.TierLabel == "" || row.StateLabel == "" || row.Says == "" || row.Expiry != "30-06-2027" {
		t.Errorf("row labels = %+v", row)
	}

	if out.Totals.Cost.Minor != 217800 || out.Totals.Units != 180 || out.Totals.Products != 1 {
		t.Errorf("totals = %+v", out.Totals)
	}
	if out.Returns.Recover.Minor != 217800 || out.Returns.OpenBatches != 1 {
		t.Errorf("returns = %+v", out.Returns)
	}
	if out.Returns.NextDeadlinePretty != "01-04-2027" {
		t.Errorf("nextDeadlinePretty = %q", out.Returns.NextDeadlinePretty)
	}
	// Fresh stock is not "about to expire".
	if out.AtRisk.Batches != 0 || out.AtRisk.Cost.Minor != 0 {
		t.Errorf("atRisk = %+v, want nothing at 121 days out", out.AtRisk)
	}

	t.Run("later in the year the same batch is at risk and stranded", func(t *testing.T) {
		rec := mustPost(t, s, "GET", "/api/board?asOf=2027-05-15", "")
		var later boardResponse
		decode(t, rec, &later)
		if later.AtRisk.Batches != 1 || later.AtRisk.Cost.Minor != 217800 {
			t.Errorf("atRisk = %+v", later.AtRisk)
		}
		if later.Returns.StrandedBatches != 1 || later.Returns.Recover.Minor != 0 {
			t.Errorf("returns = %+v, want it stranded with nothing recoverable", later.Returns)
		}
		if later.Rows[0].Verdict.Tier != stock.TierWarn {
			t.Errorf("tier = %s, want warn", later.Rows[0].Verdict.Tier)
		}
	})

	t.Run("after it dies", func(t *testing.T) {
		rec := mustPost(t, s, "GET", "/api/board?asOf=2027-07-01", "")
		var dead boardResponse
		decode(t, rec, &dead)
		if dead.Returns.DeadBatches != 1 || dead.Returns.Dead.Minor != 217800 || dead.Returns.DeadUnits != 180 {
			t.Errorf("returns = %+v", dead.Returns)
		}
		if dead.AtRisk.Batches != 0 {
			t.Error("expired stock is past risk, not at risk")
		}
	})

	t.Run("a bad as-of date", func(t *testing.T) {
		if rec := do(t, s, "GET", "/api/board?asOf=whenever", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("= %d, want 400", rec.Code)
		}
	})
}

func TestReturnsEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	seed(t, s)

	rec := mustPost(t, s, "GET", "/api/returns?asOf=2027-03-01", "")
	var out returnsResponse
	decode(t, rec, &out)
	if len(out.Rows) != 1 {
		t.Fatalf("got %d rows: %s", len(out.Rows), rec.Body.String())
	}
	if out.Recover.Minor != 217800 || out.Total.Minor != 217800 {
		t.Errorf("recover = %d, total = %d", out.Recover.Minor, out.Total.Minor)
	}
	if !strings.Contains(out.Note, "not a figure any regulator sets") {
		t.Errorf("note = %q; the honesty about the credit rate must survive to the screen", out.Note)
	}

	t.Run("past the window there is nothing to send back", func(t *testing.T) {
		rec := mustPost(t, s, "GET", "/api/returns?asOf=2027-05-15", "")
		var later returnsResponse
		decode(t, rec, &later)
		if len(later.Rows) != 0 || later.Recover.Minor != 0 {
			t.Errorf("rows = %d, recover = %d", len(later.Rows), later.Recover.Minor)
		}
	})
}

func TestPickEndpoints(t *testing.T) {
	s, store := newTestServer(t)
	seed(t, s)
	// A second, longer-dated lot of the same product.
	mustPost(t, s, "PUT", "/api/batch", `{
		"product":"Amoxicillin 500 mg capsule","pack":"strip of 10","batchNo":"AX3310",
		"expiresOn":"MAR 2028","qty":60,"unitCost":"12.10","unitPrice":"18.00","supplier":"Metro Distributors"}`)

	rec := mustPost(t, s, "GET", "/api/pick?product=Amoxicillin+500+mg+capsule&qty=200&asOf=2027-03-01", "")
	var out pickResponse
	decode(t, rec, &out)
	if out.Allocated != 200 || out.Shortfall != 0 {
		t.Fatalf("allocated %d, short %d: %s", out.Allocated, out.Shortfall, rec.Body.String())
	}
	if len(out.Lines) != 2 || out.Lines[0].BatchNo != "AX2291" {
		t.Fatalf("lines = %+v, want the June lot first", out.Lines)
	}
	if out.Lines[0].Take != 180 || out.Lines[1].Take != 20 {
		t.Errorf("takes = %d and %d", out.Lines[0].Take, out.Lines[1].Take)
	}
	if out.Value.Minor != 200*1210 {
		t.Errorf("value = %d", out.Value.Minor)
	}

	t.Run("nothing is deducted by asking", func(t *testing.T) {
		store.Read(func(bk *stock.Book) {
			for _, b := range bk.Batches {
				if b.BatchNo == "AX2291" && b.Qty != 180 {
					t.Errorf("quantity changed to %d just from building a pick list", b.Qty)
				}
			}
		})
	})

	t.Run("applying it takes the stock off the shelf", func(t *testing.T) {
		mustPost(t, s, "POST", "/api/pick/apply", `{"product":"Amoxicillin 500 mg capsule","qty":200,"asOf":"2027-03-01"}`)
		store.Read(func(bk *stock.Book) {
			for _, b := range bk.Batches {
				switch b.BatchNo {
				case "AX2291":
					if b.Qty != 0 {
						t.Errorf("AX2291 = %d, want 0", b.Qty)
					}
				case "AX3310":
					if b.Qty != 40 {
						t.Errorf("AX3310 = %d, want 40", b.Qty)
					}
				}
			}
		})
	})

	t.Run("asking for more than there is", func(t *testing.T) {
		rec := mustPost(t, s, "GET", "/api/pick?product=Amoxicillin+500+mg+capsule&qty=100&asOf=2027-03-01", "")
		var short pickResponse
		decode(t, rec, &short)
		if short.Shortfall != 60 || short.Allocated != 40 {
			t.Errorf("allocated %d, short %d", short.Allocated, short.Shortfall)
		}
	})

	cases := []struct {
		name string
		path string
	}{
		{"no product", "/api/pick?qty=5"},
		{"no quantity", "/api/pick?product=Amoxicillin+500+mg+capsule"},
		{"a quantity that is not a number", "/api/pick?product=Amoxicillin+500+mg+capsule&qty=many"},
		{"zero", "/api/pick?product=Amoxicillin+500+mg+capsule&qty=0"},
		{"an unknown product", "/api/pick?product=Ibuprofen&qty=5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := do(t, s, "GET", tc.path, ""); rec.Code != http.StatusBadRequest {
				t.Errorf("= %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}

	t.Run("the shelf order", func(t *testing.T) {
		rec := mustPost(t, s, "GET", "/api/shelf?product=amoxicillin+500+mg+capsule&asOf=2027-03-01", "")
		var shelf struct {
			Product string    `json:"product"`
			Rows    []rowView `json:"rows"`
		}
		decode(t, rec, &shelf)
		if len(shelf.Rows) != 2 || shelf.Rows[0].Batch.BatchNo != "AX2291" {
			t.Errorf("rows = %d, first = %q", len(shelf.Rows), shelf.Rows[0].Batch.BatchNo)
		}
		if rec := do(t, s, "GET", "/api/shelf", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("no product = %d, want 400", rec.Code)
		}
	})
}

func TestReadExpiryEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	cases := []struct {
		in     string
		ok     bool
		pretty string
	}{
		{"03/2027", true, "31-03-2027"},
		{"MAR 2027", true, "31-03-2027"},
		{"02/2028", true, "29-02-2028"},
		{"02/2027", true, "28-02-2027"},
		{"2027-03-15", true, "15-03-2027"},
		{"sometime", false, ""},
		{"", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			rec := mustPost(t, s, "GET", "/api/read-expiry?v="+strings.ReplaceAll(tc.in, " ", "+"), "")
			var out struct {
				OK      bool   `json:"ok"`
				Pretty  string `json:"pretty"`
				Month   string `json:"month"`
				Message string `json:"message"`
			}
			decode(t, rec, &out)
			if out.OK != tc.ok {
				t.Fatalf("ok = %v, want %v (%s)", out.OK, tc.ok, rec.Body.String())
			}
			if tc.ok && out.Pretty != tc.pretty {
				t.Errorf("pretty = %q, want %q", out.Pretty, tc.pretty)
			}
			if !tc.ok && out.Message == "" {
				t.Error("a rejection should say why")
			}
		})
	}
}

func TestPutBatchRejections(t *testing.T) {
	s, _ := newTestServer(t)
	id := seed(t, s)

	cases := []struct {
		name string
		body string
		want string
		code int
	}{
		{"no product", `{"product":"","batchNo":"X1","expiresOn":"03/2027"}`, "product name is needed", 400},
		{"no batch number", `{"product":"X","batchNo":"","expiresOn":"03/2027"}`, "batch number", 400},
		{"no expiry", `{"product":"X","batchNo":"X1","expiresOn":""}`, "expiry printed on the pack", 400},
		{"an unreadable expiry", `{"product":"X","batchNo":"X1","expiresOn":"soon"}`, "not an expiry", 400},
		{"an unreadable purchase date", `{"product":"X","batchNo":"X1","expiresOn":"03/2027","purchasedOn":"last year"}`, "not a date", 400},
		{"an unreadable cost", `{"product":"X","batchNo":"X1","expiresOn":"03/2027","unitCost":"cheap"}`, "unit cost could not be read", 400},
		{"an unreadable price", `{"product":"X","batchNo":"X1","expiresOn":"03/2027","unitPrice":"a lot"}`, "unit price could not be read", 400},
		{"a negative quantity", `{"product":"X","batchNo":"X1","expiresOn":"03/2027","qty":-5}`, "", 200},
		{"the same lot twice", `{"product":"Amoxicillin 500 mg capsule","batchNo":"AX2291","expiresOn":"06/2027"}`, "already on file", 400},
		{"an unknown id", `{"id":"b99","product":"X","batchNo":"X9","expiresOn":"03/2027"}`, "no batch with that id", 404},
		{"a field the tool does not know", `{"product":"X","batchNo":"X1","expiresOn":"03/2027","colour":"blue"}`, "could not be read", 400},
		{"two objects in one body", `{"product":"X","batchNo":"X1","expiresOn":"03/2027"}{"product":"Y"}`, "more than one object", 400},
		{"not JSON at all", `product,batch`, "could not be read", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, s, "PUT", "/api/batch", tc.body)
			if rec.Code != tc.code {
				t.Fatalf("= %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
			if tc.want != "" && !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body = %s, want it to mention %q", rec.Body.String(), tc.want)
			}
		})
	}

	t.Run("a batch with no purchase date is saved with a warning", func(t *testing.T) {
		rec := mustPost(t, s, "PUT", "/api/batch", `{"product":"Cetirizine 10 mg","batchNo":"CT1","expiresOn":"11/2027","qty":90,"unitCost":"4.50"}`)
		var out struct {
			Warnings []string `json:"warnings"`
		}
		decode(t, rec, &out)
		if len(out.Warnings) == 0 {
			t.Fatal("want a warning about the credit-note cut-off")
		}
		if !strings.Contains(strings.Join(out.Warnings, " "), "credit-note cut-off") {
			t.Errorf("warnings = %v", out.Warnings)
		}
	})

	t.Run("an edit keeps the id", func(t *testing.T) {
		rec := mustPost(t, s, "PUT", "/api/batch", `{"id":"`+id+`","product":"Amoxicillin 500 mg capsule","batchNo":"AX2291","expiresOn":"06/2027","qty":90,"unitCost":"12.10","unitPrice":"18.00","supplier":"Metro Distributors"}`)
		var out struct {
			Batch stock.Batch `json:"batch"`
		}
		decode(t, rec, &out)
		if out.Batch.ID != id || out.Batch.Qty != 90 {
			t.Errorf("saved %+v", out.Batch)
		}
	})
}

func TestDeleteAndSettleBatch(t *testing.T) {
	s, store := newTestServer(t)
	id := seed(t, s)

	t.Run("settling", func(t *testing.T) {
		mustPost(t, s, "POST", "/api/batch/settle", `{"id":"`+id+`","action":"returned","on":"2027-03-10"}`)
		store.Read(func(bk *stock.Book) {
			if got := bk.Find(id); got == nil || got.ReturnedOn != stock.NewDate(2027, time.March, 10) {
				t.Errorf("returnedOn = %+v", got)
			}
		})
		// It drops off the board, and into the loss log's rescued column.
		rec := mustPost(t, s, "GET", "/api/board?asOf=2027-03-11", "")
		var out boardResponse
		decode(t, rec, &out)
		if len(out.Rows) != 0 {
			t.Errorf("a returned batch is still on the board")
		}
		if out.Loss.ReturnedBatches != 1 || out.Loss.Returned.Minor != 217800 {
			t.Errorf("loss = %+v", out.Loss)
		}

		mustPost(t, s, "POST", "/api/batch/settle", `{"id":"`+id+`","action":"reopen"}`)
		mustPost(t, s, "POST", "/api/batch/settle", `{"id":"`+id+`","action":"writeOff","on":"2027-07-05"}`)
		rec = mustPost(t, s, "GET", "/api/board?asOf=2027-07-06", "")
		decode(t, rec, &out)
		if out.Loss.Batches != 1 || out.Loss.Cost.Minor != 217800 {
			t.Errorf("loss = %+v", out.Loss)
		}
		if out.Loss.Share == "" {
			t.Error("the share of stock was not formatted")
		}
	})

	cases := []struct {
		name string
		body string
		code int
	}{
		{"an unknown id", `{"id":"b99","action":"returned"}`, 404},
		{"no id", `{"id":"","action":"returned"}`, 400},
		{"an unknown action", `{"id":"` + id + `","action":"burn"}`, 400},
		{"a bad date", `{"id":"` + id + `","action":"returned","on":"whenever"}`, 400},
		{"writing off twice", `{"id":"` + id + `","action":"writeOff"}`, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := do(t, s, "POST", "/api/batch/settle", tc.body); rec.Code != tc.code {
				t.Errorf("= %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
		})
	}

	t.Run("deleting", func(t *testing.T) {
		mustPost(t, s, "DELETE", "/api/batch?id="+id, "")
		store.Read(func(bk *stock.Book) {
			if bk.Find(id) != nil {
				t.Error("the batch is still there")
			}
		})
		if rec := do(t, s, "DELETE", "/api/batch?id="+id, ""); rec.Code != http.StatusNotFound {
			t.Errorf("deleting twice = %d, want 404", rec.Code)
		}
		if rec := do(t, s, "DELETE", "/api/batch", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("no id = %d, want 400", rec.Code)
		}
	})
}

func TestPutPolicy(t *testing.T) {
	s, store := newTestServer(t)
	seed(t, s)

	good := `{"market":"PH","watchDays":120,"warnDays":45,"urgentDays":7,
		"returnOpensDays":240,"returnClosesDays":60,"returnLeadDays":10,
		"creditPercent":85,"closingSoonDays":21,"lossWindowDays":180,"applyCreditNoteCutoff":true}`
	mustPost(t, s, "PUT", "/api/policy", good)
	store.Read(func(bk *stock.Book) {
		p := bk.Policy
		if p.Market != stock.Philippines || p.CreditPercent != 85 || p.ReturnLeadDays != 10 || p.WatchDays != 120 {
			t.Errorf("policy = %+v", p)
		}
		if p.ApplyCreditNoteCutoff {
			t.Error("the Indian cut-off must be switched off for a Philippine shop even if the request asks for it")
		}
	})

	t.Run("the board is worked out again from the new figures", func(t *testing.T) {
		rec := mustPost(t, s, "GET", "/api/board?asOf=2027-03-01", "")
		var out boardResponse
		decode(t, rec, &out)
		if out.Currency != "PHP" {
			t.Errorf("currency = %q, want PHP", out.Currency)
		}
		row := out.Rows[0]
		if !strings.HasPrefix(row.Cost.Text, "₱") {
			t.Errorf("cost = %q, want pesos", row.Cost.Text)
		}
		// The window now runs 240 to 60 days before 30 June 2027, with a
		// ten-day lead time, and credit is 85% of cost.
		if row.Verdict.Return.ClosesOn != stock.NewDate(2027, time.May, 1) {
			t.Errorf("closesOn = %s, want 2027-05-01", row.Verdict.Return.ClosesOn)
		}
		if row.Verdict.Return.Deadline != stock.NewDate(2027, time.April, 21) {
			t.Errorf("deadline = %s, want 2027-04-21 after the lead time", row.Verdict.Return.Deadline)
		}
		if row.Recoverable.Minor != 185130 {
			t.Errorf("recoverable = %d, want 85%% of 217800", row.Recoverable.Minor)
		}
		// 1 March is 121 days before 30 June, one day outside the new watch
		// tier; 2 March is exactly on it.
		if row.Verdict.DaysLeft != 121 || row.Verdict.Tier != stock.TierFresh {
			t.Errorf("tier = %s at %d days left, want fresh at 121", row.Verdict.Tier, row.Verdict.DaysLeft)
		}
		next := mustPost(t, s, "GET", "/api/board?asOf=2027-03-02", "")
		var edge boardResponse
		decode(t, next, &edge)
		if edge.Rows[0].Verdict.Tier != stock.TierWatch {
			t.Errorf("tier = %s at 120 days left, want watch under a 120 day watch tier", edge.Rows[0].Verdict.Tier)
		}
	})

	cases := []struct {
		name string
		body string
	}{
		{"an unknown market", `{"market":"US","watchDays":90,"warnDays":60,"urgentDays":30,"returnOpensDays":180,"returnClosesDays":90,"returnLeadDays":0,"creditPercent":100,"closingSoonDays":14,"lossWindowDays":365}`},
		{"tiers out of order", `{"market":"IN","watchDays":30,"warnDays":60,"urgentDays":90,"returnOpensDays":180,"returnClosesDays":90,"returnLeadDays":0,"creditPercent":100,"closingSoonDays":14,"lossWindowDays":365}`},
		{"a window that opens after it closes", `{"market":"IN","watchDays":90,"warnDays":60,"urgentDays":30,"returnOpensDays":60,"returnClosesDays":90,"returnLeadDays":0,"creditPercent":100,"closingSoonDays":14,"lossWindowDays":365}`},
		{"a lead time wider than the window", `{"market":"IN","watchDays":90,"warnDays":60,"urgentDays":30,"returnOpensDays":180,"returnClosesDays":90,"returnLeadDays":200,"creditPercent":100,"closingSoonDays":14,"lossWindowDays":365}`},
		{"a credit rate over a hundred", `{"market":"IN","watchDays":90,"warnDays":60,"urgentDays":30,"returnOpensDays":180,"returnClosesDays":90,"returnLeadDays":0,"creditPercent":120,"closingSoonDays":14,"lossWindowDays":365}`},
		{"a field the tool does not know", `{"market":"IN","watchDays":90,"warnDays":60,"urgentDays":30,"returnOpensDays":180,"returnClosesDays":90,"returnLeadDays":0,"creditPercent":100,"closingSoonDays":14,"lossWindowDays":365,"discount":5}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := do(t, s, "PUT", "/api/policy", tc.body); rec.Code != http.StatusBadRequest {
				t.Errorf("= %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestImportAndExportCSV(t *testing.T) {
	s, store := newTestServer(t)
	seed(t, s)

	sheet := strings.Join([]string{
		"Item Name,Packing,Batch No,Exp,Qty,Rate,MRP,Distributor,Rack,Invoice Date",
		"Paracetamol 650 mg tablet,strip of 15,PC8842,03/2027,420,1.50,2.00,Southern Pharma,B1,15-09-2026",
		"Cetirizine 10 mg tablet,strip of 10,CT119,2027-11-30,90,4.50,6.00,Metro Distributors,C2,",
		",BAD,03/2027,10,1.00,2.00,,,",
	}, "\n") + "\n"

	req := httptest.NewRequest("POST", "/api/import.csv", strings.NewReader(sheet))
	req.Header.Set("Content-Type", "text/csv")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import = %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Imported int             `json:"imported"`
		Read     int             `json:"read"`
		Skipped  []stock.Problem `json:"skipped"`
		Columns  []string        `json:"columns"`
		Replaced bool            `json:"replaced"`
	}
	decode(t, rec, &out)
	if out.Imported != 2 || out.Read != 3 {
		t.Errorf("imported %d of %d read", out.Imported, out.Read)
	}
	if len(out.Skipped) != 1 || out.Skipped[0].Line != 4 {
		t.Errorf("skipped = %+v, want line 4", out.Skipped)
	}
	if !out.Replaced {
		t.Error("the response should say the list was replaced, not merged")
	}

	store.Read(func(bk *stock.Book) {
		if len(bk.Batches) != 2 {
			t.Fatalf("the book holds %d batches, want the two imported ones", len(bk.Batches))
		}
		for _, b := range bk.Batches {
			if b.BatchNo == "AX2291" {
				t.Error("the import merged into the old list instead of replacing it")
			}
		}
		if bk.Batches[0].ExpiresOn != stock.NewDate(2027, time.March, 31) {
			t.Errorf("a month-only expiry became %s, want 2027-03-31", bk.Batches[0].ExpiresOn)
		}
	})

	t.Run("a file with nothing readable leaves the list alone", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/import.csv", strings.NewReader("product,batch_no,expiry\n,,\n"))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("= %d, want 400: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "left alone") {
			t.Errorf("body = %s", rec.Body.String())
		}
		store.Read(func(bk *stock.Book) {
			if len(bk.Batches) != 2 {
				t.Errorf("the book now holds %d batches", len(bk.Batches))
			}
		})
	})

	t.Run("a file with no usable header", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/import.csv", strings.NewReader("a,b,c\n1,2,3\n"))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("= %d, want 400", rec.Code)
		}
	})

	t.Run("export", func(t *testing.T) {
		rec := mustPost(t, s, "GET", "/api/export.csv?asOf=2027-01-15", "")
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
			t.Errorf("Content-Type = %q", got)
		}
		if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "batches.csv") {
			t.Errorf("Content-Disposition = %q", got)
		}
		body := rec.Body.String()
		if !strings.HasPrefix(body, "product,pack,batch_no,expires_on") {
			t.Errorf("the export does not start with the expected header: %.80s", body)
		}
		if !strings.Contains(body, "PC8842") || !strings.Contains(body, "CT119") {
			t.Error("the export is missing a batch")
		}
		if strings.Count(strings.TrimSpace(body), "\n") != 2 {
			t.Errorf("want a header and two rows, got:\n%s", body)
		}
		if rec := do(t, s, "GET", "/api/export.csv?asOf=nonsense", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("a bad as-of = %d, want 400", rec.Code)
		}
	})
}

func TestSharePercentFormatting(t *testing.T) {
	cases := []struct {
		perMille int
		want     string
	}{
		{0, "0.0%"},
		{5, "0.5%"},
		{10, "1.0%"},
		{113, "11.3%"},
		{1000, "100.0%"},
		{-25, "-2.5%"},
	}
	for _, tc := range cases {
		if got := sharePercent(tc.perMille); got != tc.want {
			t.Errorf("sharePercent(%d) = %q, want %q", tc.perMille, got, tc.want)
		}
	}
}

func TestRunRejectsAnAddressItCannotHave(t *testing.T) {
	// A port that is already taken is the one failure path run() can be made to
	// take without actually serving: it must report it rather than panic.
	listener := httptest.NewServer(http.NotFoundHandler())
	defer listener.Close()
	addr := strings.TrimPrefix(listener.URL, "http://")
	if err := run(addr, tempData(t)); err == nil {
		t.Error("want an error when the address is already in use")
	}
}
