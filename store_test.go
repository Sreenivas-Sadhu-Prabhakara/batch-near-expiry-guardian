package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sreenivas-sadhu-prabhakara/batch-near-expiry-guardian/internal/stock"
)

func tempData(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "guardian-data.json")
}

func TestOpenStoreOnAFreshMachine(t *testing.T) {
	path := tempData(t)
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the data file was created before anything was saved")
	}
	if !filepath.IsAbs(store.Path()) {
		t.Errorf("Path() = %q, want an absolute path so the interface can show it", store.Path())
	}
	store.Read(func(bk *stock.Book) {
		if len(bk.Batches) != 0 {
			t.Errorf("a new book holds %d batches", len(bk.Batches))
		}
		if bk.Policy.WatchDays != stock.DefaultWatchDays {
			t.Errorf("a new book should start on the default policy, got %+v", bk.Policy)
		}
	})
}

func TestStoreSavesAndReloads(t *testing.T) {
	path := tempData(t)
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	batch := stock.Batch{
		Product: "Amoxicillin 500 mg", BatchNo: "AX2291",
		ExpiresOn: stock.NewDate(2027, time.June, 30), PurchasedOn: stock.NewDate(2026, time.August, 1),
		Qty: 180, UnitCost: 1210, UnitPrice: 1800, Supplier: "Metro Distributors",
	}
	if err := store.Write(func(bk *stock.Book) error {
		_, err := bk.Upsert(batch)
		return err
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the data file was not written: %v", err)
	}
	if !strings.Contains(string(raw), `"expiresOn": "2027-06-30"`) {
		t.Errorf("dates should be stored as readable ISO strings; file was:\n%s", raw)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Error("the file should end with a newline")
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("file mode = %v, want 0600: it holds purchase prices and supplier terms", mode)
		}
	}

	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	reopened.Read(func(bk *stock.Book) {
		if len(bk.Batches) != 1 {
			t.Fatalf("reloaded %d batches", len(bk.Batches))
		}
		got := bk.Batches[0]
		if got.BatchNo != "AX2291" || got.ExpiresOn != batch.ExpiresOn || got.UnitCost != 1210 || got.Qty != 180 {
			t.Errorf("reloaded %+v", got)
		}
	})

	t.Run("no temporary files are left behind", func(t *testing.T) {
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".tmp-") {
				t.Errorf("left behind %s", entry.Name())
			}
		}
	})
}

func TestStoreWriteRollsBackAFailedEdit(t *testing.T) {
	store, err := OpenStore(tempData(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	err = store.Write(func(bk *stock.Book) error {
		_, err := bk.Upsert(stock.Batch{Product: "No Expiry", BatchNo: "X1"})
		return err
	})
	if err == nil {
		t.Fatal("a batch with no expiry should be refused")
	}
	store.Read(func(bk *stock.Book) {
		if len(bk.Batches) != 0 {
			t.Error("a refused edit still changed the book")
		}
	})
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Error("a refused edit still wrote the file")
	}
}

func TestOpenStoreHandlesAwkwardFiles(t *testing.T) {
	t.Run("an empty file", func(t *testing.T) {
		path := tempData(t)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		store, err := OpenStore(path)
		if err != nil {
			t.Fatalf("an empty file should start an empty book, got %v", err)
		}
		store.Read(func(bk *stock.Book) {
			if bk.Policy.WatchDays != stock.DefaultWatchDays {
				t.Error("the policy was not defaulted")
			}
		})
	})

	t.Run("something that is not JSON", func(t *testing.T) {
		path := tempData(t)
		if err := os.WriteFile(path, []byte("product,batch\nX,1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := OpenStore(path)
		if err == nil {
			t.Fatal("want an error rather than a silently empty book")
		}
		if !strings.Contains(err.Error(), "not valid JSON") {
			t.Errorf("error = %q", err)
		}
	})

	t.Run("a file edited by hand", func(t *testing.T) {
		path := tempData(t)
		body := `{"batches":[{"product":"  Metformin 500 ","batchNo":" mf1 ","expiresOn":"2027-06-30","qty":20,"unitCost":500}]}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		store, err := OpenStore(path)
		if err != nil {
			t.Fatalf("OpenStore: %v", err)
		}
		store.Read(func(bk *stock.Book) {
			if len(bk.Batches) != 1 {
				t.Fatalf("loaded %d batches", len(bk.Batches))
			}
			got := bk.Batches[0]
			if got.ID == "" {
				t.Error("a hand-written batch was not given an id")
			}
			if got.Product != "Metformin 500" || got.BatchNo != "MF1" {
				t.Errorf("fields were not tidied: %+v", got)
			}
			if bk.Policy.CreditPercent != stock.DefaultCreditPercent {
				t.Error("the missing policy was not filled in")
			}
		})
	})

	t.Run("a bad expiry in the file", func(t *testing.T) {
		path := tempData(t)
		if err := os.WriteFile(path, []byte(`{"batches":[{"product":"X","batchNo":"1","expiresOn":"whenever"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenStore(path); err == nil {
			t.Fatal("want an error for an unreadable date rather than a silent zero")
		}
	})
}

func TestReplaceBatchesKeepsThePolicy(t *testing.T) {
	store, err := OpenStore(tempData(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	mine := stock.DefaultPolicy()
	mine.Market = stock.Philippines
	mine.CreditPercent = 85
	mine.ReturnClosesDays = 60
	mine.Normalise()
	if err := store.Write(func(bk *stock.Book) error {
		bk.Policy = mine
		return nil
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := store.Write(func(bk *stock.Book) error {
		_, err := bk.Upsert(stock.Batch{Product: "Old Stock", BatchNo: "OLD1", ExpiresOn: stock.NewDate(2027, time.January, 31), Qty: 5, UnitCost: 100})
		return err
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	replacement := []stock.Batch{
		{Product: "New Stock", BatchNo: "NEW1", ExpiresOn: stock.NewDate(2028, time.March, 31), Qty: 10, UnitCost: 200},
		{Product: "Also New", BatchNo: "NEW2", ExpiresOn: stock.NewDate(2028, time.April, 30), Qty: 20, UnitCost: 300},
	}
	if err := store.ReplaceBatches(replacement); err != nil {
		t.Fatalf("ReplaceBatches: %v", err)
	}

	store.Read(func(bk *stock.Book) {
		if len(bk.Batches) != 2 {
			t.Fatalf("holds %d batches, want the two from the sheet", len(bk.Batches))
		}
		for _, b := range bk.Batches {
			if b.ID == "" {
				t.Errorf("%s was not given an id", b.BatchNo)
			}
			if b.BatchNo == "OLD1" {
				t.Error("the old stock list was merged into rather than replaced")
			}
		}
		if bk.Policy.CreditPercent != 85 || bk.Policy.Market != stock.Philippines || bk.Policy.ReturnClosesDays != 60 {
			t.Errorf("the shop's own figures were lost: %+v", bk.Policy)
		}
	})

	// And the policy survives a round trip through the file.
	reopened, err := OpenStore(store.Path())
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	reopened.Read(func(bk *stock.Book) {
		if bk.Policy.CreditPercent != 85 || bk.Policy.Market != stock.Philippines {
			t.Errorf("after reloading: %+v", bk.Policy)
		}
	})
}

func TestStoredFileIsReadableJSON(t *testing.T) {
	store, err := OpenStore(tempData(t))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if err := store.Write(func(bk *stock.Book) error {
		_, err := bk.Upsert(stock.Batch{Product: "Paracetamol 650 mg", BatchNo: "PC1", ExpiresOn: stock.NewDate(2027, time.September, 30), Qty: 60, UnitCost: 450})
		return err
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var into struct {
		Policy  stock.Policy  `json:"policy"`
		Batches []stock.Batch `json:"batches"`
		Seq     int           `json:"seq"`
	}
	if err := json.Unmarshal(raw, &into); err != nil {
		t.Fatalf("the file does not read back as JSON: %v", err)
	}
	if into.Seq != 1 || len(into.Batches) != 1 || into.Policy.Market != stock.India {
		t.Errorf("decoded %+v", into)
	}
	if !strings.Contains(string(raw), "\n  ") {
		t.Error("the file should be indented so it can be read in a text editor")
	}
}
