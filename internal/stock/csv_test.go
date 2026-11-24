package stock

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"
)

func TestExportCSV(t *testing.T) {
	bk := boardBook(t)
	var out strings.Builder
	if err := ExportCSV(&out, bk, boardAsOf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	rows, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
	if err != nil {
		t.Fatalf("the export is not valid CSV: %v", err)
	}
	if len(rows) != len(bk.Batches)+1 {
		t.Fatalf("got %d rows, want a header plus %d batches", len(rows), len(bk.Batches))
	}

	header := rows[0]
	index := map[string]int{}
	for i, name := range header {
		index[name] = i
	}
	for _, want := range exportHeader {
		if _, ok := index[want]; !ok {
			t.Errorf("the header is missing %q", want)
		}
	}

	find := func(batchNo string) []string {
		t.Helper()
		for _, row := range rows[1:] {
			if row[index["batch_no"]] == batchNo {
				return row
			}
		}
		t.Fatalf("no row for batch %s", batchNo)
		return nil
	}

	t.Run("a batch inside the return window", func(t *testing.T) {
		row := find("BB2")
		cases := []struct {
			column string
			want   string
		}{
			{"product", "Watch Tablet 10 mg"},
			{"pack", "strip of 10"},
			{"expires_on", "2027-05-30"},
			{"qty", "50"},
			{"unit_cost", "20.00"},
			{"unit_price", "30.00"},
			{"tier", "watch"},
			{"days_left", "90"},
			{"cost_value", "1000.00"},
			{"retail_value", "1500.00"},
			{"return_state", "open"},
			{"return_deadline", "2027-03-01"},
			{"deadline_set_by", "window"},
			{"recoverable", "1000.00"},
			{"status", "on shelf"},
		}
		for _, tc := range cases {
			if got := row[index[tc.column]]; got != tc.want {
				t.Errorf("%s = %q, want %q", tc.column, got, tc.want)
			}
		}
		if !strings.Contains(row[index["action"]], "Return to Metro Distributors") {
			t.Errorf("action = %q", row[index["action"]])
		}
	})

	t.Run("an expired batch reports negative days", func(t *testing.T) {
		row := find("BB5")
		if got := row[index["days_left"]]; got != "-1" {
			t.Errorf("days_left = %q, want -1", got)
		}
		if got := row[index["tier"]]; got != "expired" {
			t.Errorf("tier = %q", got)
		}
		if got := row[index["recoverable"]]; got != "0.00" {
			t.Errorf("recoverable = %q, want 0.00", got)
		}
	})

	t.Run("a settled batch reports its status and no action", func(t *testing.T) {
		if got := find("BB7")[index["status"]]; got != "written off 2027-02-10" {
			t.Errorf("status = %q", got)
		}
		if got := find("BB8")[index["status"]]; got != "returned 2027-02-20" {
			t.Errorf("status = %q", got)
		}
		if got := find("BB7")[index["action"]]; got != "" {
			t.Errorf("action = %q, want nothing to do on a settled batch", got)
		}
	})

	t.Run("money is written plain so a spreadsheet can add it up", func(t *testing.T) {
		for _, row := range rows[1:] {
			for _, column := range []string{"unit_cost", "unit_price", "cost_value", "retail_value", "recoverable"} {
				value := row[index[column]]
				if strings.ContainsAny(value, ",₹₱") {
					t.Errorf("%s = %q; it must carry no sign and no grouping", column, value)
				}
			}
		}
	})

	t.Run("an empty book still writes the header", func(t *testing.T) {
		var empty strings.Builder
		if err := ExportCSV(&empty, NewBook(), boardAsOf); err != nil {
			t.Fatalf("ExportCSV: %v", err)
		}
		got, err := csv.NewReader(strings.NewReader(empty.String())).ReadAll()
		if err != nil {
			t.Fatalf("not valid CSV: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("got %d rows, want just the header", len(got))
		}
	})
}

func TestImportCSVReadsARealStockSheet(t *testing.T) {
	// The header a shop's own sheet actually carries: nothing canonical.
	sheet := strings.Join([]string{
		"Item Name,Packing,Batch No,Exp,Qty,Rate,MRP,Distributor,Rack,Invoice Date",
		"Amoxicillin 500 mg capsule,strip of 10,AX2291,03/2027,180,12.10,18.00,Metro Distributors,A3,01-08-2026",
		"Paracetamol 650 mg tablet,strip of 15,PC8842,MAR 2028,420,\"1,250.00\",1500.00,Southern Pharma,B1,2026-09-15",
		"Cetirizine 10 mg tablet,strip of 10,CT119,2027-11-30,90,4.50,6.00,Metro Distributors,C2,",
	}, "\n") + "\n"

	result, err := ImportCSV(strings.NewReader(sheet))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("skipped lines: %+v", result.Skipped)
	}
	if result.Read != 3 || len(result.Batches) != 3 {
		t.Fatalf("read %d lines into %d batches, want 3 and 3", result.Read, len(result.Batches))
	}

	first := result.Batches[0]
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"product", first.Product, "Amoxicillin 500 mg capsule"},
		{"pack", first.Pack, "strip of 10"},
		{"batch number", first.BatchNo, "AX2291"},
		// A month-only expiry becomes the last day of that month.
		{"expiry", first.ExpiresOn, NewDate(2027, time.March, 31)},
		{"purchase date read day-first", first.PurchasedOn, NewDate(2026, time.August, 1)},
		{"quantity", first.Qty, 180},
		{"unit cost", first.UnitCost, Money(1210)},
		{"unit price", first.UnitPrice, Money(1800)},
		{"supplier", first.Supplier, "Metro Distributors"},
		{"shelf", first.Shelf, "A3"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}

	second := result.Batches[1]
	if second.ExpiresOn != NewDate(2028, time.March, 31) {
		t.Errorf("a named month expiry = %s, want 2028-03-31", second.ExpiresOn)
	}
	if second.UnitCost != Money(125000) {
		t.Errorf("a grouped amount = %d, want 125000", second.UnitCost)
	}
	if second.PurchasedOn != NewDate(2026, time.September, 15) {
		t.Errorf("an ISO purchase date = %s", second.PurchasedOn)
	}

	third := result.Batches[2]
	if third.ExpiresOn != NewDate(2027, time.November, 30) {
		t.Errorf("a full expiry date = %s, want it taken literally", third.ExpiresOn)
	}
	if !third.PurchasedOn.IsZero() {
		t.Errorf("a blank purchase date = %s, want the zero date", third.PurchasedOn)
	}

	for _, want := range []string{"product", "batch_no", "expires_on", "qty", "unit_cost", "unit_price", "supplier", "shelf", "pack", "purchased_on"} {
		var found bool
		for _, got := range result.Columns {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("column %q was not reported as matched; got %v", want, result.Columns)
		}
	}
}

func TestImportCSVColumnAliases(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"canonical", "product,batch_no,expires_on"},
		{"shop shorthand", "item,lot no,exp"},
		{"long form", "product name,batch number,date of expiry"},
		{"mixed case and spacing", "  Product  ,  BATCH NO ,  Expiry Date  "},
		{"with full stops", "particulars,batch no.,exp. date"},
		{"byte order mark on the first column", "\xef\xbb\xbfproduct,batch_no,expiry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.header + "\nMetformin 500 mg,MF4471,06/2027\n"
			result, err := ImportCSV(strings.NewReader(body))
			if err != nil {
				t.Fatalf("ImportCSV: %v", err)
			}
			if len(result.Batches) != 1 {
				t.Fatalf("read %d batches, skipped %+v", len(result.Batches), result.Skipped)
			}
			got := result.Batches[0]
			if got.Product != "Metformin 500 mg" || got.BatchNo != "MF4471" || got.ExpiresOn != NewDate(2027, time.June, 30) {
				t.Errorf("read %+v", got)
			}
		})
	}
}

func TestImportCSVRequiresTheColumnsItCannotInvent(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		want   string
		isFile bool
	}{
		{"no product column", "batch_no,expiry\nAX1,03/2027\n", "needs a product column", true},
		{"no batch column", "product,expiry\nAmoxicillin,03/2027\n", "needs a batch no column", true},
		{"no expiry column", "product,batch_no\nAmoxicillin,AX1\n", "needs a expires on column", true},
		{"an unrecognisable header", "a,b,c\n1,2,3\n", "needs a", true},
		{"an empty file", "", "the file is empty", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ImportCSV(strings.NewReader(tc.body))
			if err == nil {
				t.Fatalf("want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
			if tc.isFile && !strings.Contains(err.Error(), "the header read was") {
				t.Errorf("error = %q, want it to echo the header so the owner can see what was read", err)
			}
		})
	}
}

func TestImportCSVReportsBadLinesWithTheirLineNumbers(t *testing.T) {
	sheet := strings.Join([]string{
		"product,batch_no,expiry,qty,rate,mrp,purchase date", // line 1
		"Good One,G1,03/2027,10,5.00,8.00,01-01-2026",        // line 2: fine
		",B1,03/2027,10,5.00,8.00,",                          // line 3: no product
		"No Batch,,03/2027,10,5.00,8.00,",                    // line 4: no batch number
		"No Expiry,N1,,10,5.00,8.00,",                        // line 5: no expiry
		"Bad Expiry,X1,sometime,10,5.00,8.00,",               // line 6: unreadable expiry
		"Bad Qty,Q1,03/2027,ten,5.00,8.00,",                  // line 7: unreadable quantity
		"Bad Cost,C1,03/2027,10,five,8.00,",                  // line 8: unreadable cost
		"Bad Price,P1,03/2027,10,5.00,eight,",                // line 9: unreadable price
		"Bad Purchase,U1,03/2027,10,5.00,8.00,whenever",      // line 10: unreadable purchase date
		"Future Purchase,F1,03/2027,10,5.00,8.00,01-01-2028", // line 11: bought after it expires
		"",                                  // line 12: blank, ignored entirely
		"Good Two,G2,04/2027,20,6.00,9.00,", // line 13: fine
	}, "\n") + "\n"

	result, err := ImportCSV(strings.NewReader(sheet))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if len(result.Batches) != 2 {
		t.Fatalf("read %d batches, want 2: %+v", len(result.Batches), result.Batches)
	}
	if result.Read != 11 {
		t.Errorf("Read = %d, want 11 non-blank lines", result.Read)
	}

	want := map[int]string{
		3:  "product name is needed",
		4:  "batch number",
		5:  "no expiry date on this line",
		6:  `expiry "sometime" could not be read`,
		7:  `quantity "ten" is not a whole number`,
		8:  `unit cost "five" could not be read`,
		9:  `unit price "eight" could not be read`,
		10: `purchase date "whenever" could not be read`,
		11: "after the expiry date",
	}
	got := map[int]string{}
	for _, p := range result.Skipped {
		got[p.Line] = p.Message
	}
	if len(got) != len(want) {
		t.Errorf("skipped %d lines, want %d: %+v", len(got), len(want), result.Skipped)
	}
	for line, fragment := range want {
		if !strings.Contains(got[line], fragment) {
			t.Errorf("line %d skipped with %q, want it to mention %q", line, got[line], fragment)
		}
	}
	// A blank line is not a problem and is not counted as one.
	if _, reported := got[12]; reported {
		t.Error("a blank line was reported as a problem")
	}
}

func TestImportCSVRefusesASecondRowForOneLot(t *testing.T) {
	sheet := strings.Join([]string{
		"product,batch_no,expiry,qty",
		"Amoxicillin 500 mg,AX2291,03/2027,100",
		"amoxicillin  500 MG,ax2291,03/2027,40", // the same lot, typed differently
		"Amoxicillin 500 mg,AX3310,09/2027,60",  // a different lot of the same product
		"Paracetamol 650 mg,AX2291,03/2027,25",  // the same batch number, different product
	}, "\n") + "\n"

	result, err := ImportCSV(strings.NewReader(sheet))
	if err != nil {
		t.Fatalf("ImportCSV: %v", err)
	}
	if len(result.Batches) != 3 {
		t.Fatalf("read %d batches, want 3: %+v", len(result.Batches), result.Batches)
	}
	if result.Duplicate != 1 {
		t.Errorf("Duplicate = %d, want 1", result.Duplicate)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Line != 3 {
		t.Fatalf("skipped = %+v, want line 3 only", result.Skipped)
	}
	if !strings.Contains(result.Skipped[0].Message, "already on line 2") {
		t.Errorf("message = %q, want it to point at the first row", result.Skipped[0].Message)
	}
}

func TestImportThenExportRoundTrips(t *testing.T) {
	bk := boardBook(t)
	var exported strings.Builder
	if err := ExportCSV(&exported, bk, boardAsOf); err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	result, err := ImportCSV(strings.NewReader(exported.String()))
	if err != nil {
		t.Fatalf("ImportCSV of our own export: %v", err)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("our own export does not read back: %+v", result.Skipped)
	}
	if len(result.Batches) != len(bk.Batches) {
		t.Fatalf("read %d batches back, want %d", len(result.Batches), len(bk.Batches))
	}

	for i, got := range result.Batches {
		want := bk.Batches[i]
		cases := []struct {
			name string
			got  any
			want any
		}{
			{"product", got.Product, want.Product},
			{"pack", got.Pack, want.Pack},
			{"batch number", got.BatchNo, want.BatchNo},
			{"expiry", got.ExpiresOn, want.ExpiresOn},
			{"purchase date", got.PurchasedOn, want.PurchasedOn},
			{"quantity", got.Qty, want.Qty},
			{"unit cost", got.UnitCost, want.UnitCost},
			{"unit price", got.UnitPrice, want.UnitPrice},
			{"supplier", got.Supplier, want.Supplier},
			{"shelf", got.Shelf, want.Shelf},
		}
		for _, tc := range cases {
			if tc.got != tc.want {
				t.Errorf("batch %d %s = %v, want %v", i, tc.name, tc.got, tc.want)
			}
		}
	}
}

func TestImportCSVHandlesAwkwardFiles(t *testing.T) {
	t.Run("short rows are read as far as they go", func(t *testing.T) {
		sheet := "product,batch_no,expiry,qty,rate\nAmoxicillin,AX1,03/2027\n"
		result, err := ImportCSV(strings.NewReader(sheet))
		if err != nil {
			t.Fatalf("ImportCSV: %v", err)
		}
		if len(result.Batches) != 1 {
			t.Fatalf("skipped %+v", result.Skipped)
		}
		if result.Batches[0].Qty != 0 || result.Batches[0].UnitCost != 0 {
			t.Errorf("missing columns should read as zero, got %+v", result.Batches[0])
		}
	})

	t.Run("a header with no rows", func(t *testing.T) {
		result, err := ImportCSV(strings.NewReader("product,batch_no,expiry\n"))
		if err != nil {
			t.Fatalf("ImportCSV: %v", err)
		}
		if len(result.Batches) != 0 || result.Batches == nil {
			t.Errorf("want an empty slice, got %v", result.Batches)
		}
	})

	t.Run("quantities with grouping", func(t *testing.T) {
		result, err := ImportCSV(strings.NewReader("product,batch_no,expiry,qty\nX,X1,03/2027,\"1,200\"\n"))
		if err != nil {
			t.Fatalf("ImportCSV: %v", err)
		}
		if len(result.Batches) != 1 || result.Batches[0].Qty != 1200 {
			t.Errorf("got %+v, skipped %+v", result.Batches, result.Skipped)
		}
	})

	t.Run("a repeated column takes the first one", func(t *testing.T) {
		result, err := ImportCSV(strings.NewReader("product,batch_no,expiry,qty,quantity\nX,X1,03/2027,10,999\n"))
		if err != nil {
			t.Fatalf("ImportCSV: %v", err)
		}
		if result.Batches[0].Qty != 10 {
			t.Errorf("Qty = %d, want 10", result.Batches[0].Qty)
		}
	})
}
