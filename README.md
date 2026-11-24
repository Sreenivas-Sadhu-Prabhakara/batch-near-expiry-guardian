# Batch & Near-Expiry Guardian

Track pharmacy stock by **batch and expiry date**, put a money figure on what is
about to die, and catch your distributor's **return window** before it shuts.

One binary, no dependencies, no internet. It serves a small interface on
`127.0.0.1` and keeps everything in a plain JSON file on the same machine.

Explainer page: <https://sreenivas-sadhu-prabhakara.github.io/batch-near-expiry-guardian/>

## What it does

A quantity on a spreadsheet cannot tell you that 180 of your 240 amoxicillin
capsules are the batch that dies in March. Record each lot as it arrives —
product, batch number, expiry, quantity, cost, retail price, supplier, shelf —
and for any date you ask about the tool works out:

- **What is about to die, in money.** Value at cost of every batch inside an
  alert tier, with the sale that will not happen beside it.
- **What can still go back for credit.** Every batch inside the return window,
  earliest deadline first, with the credit you would expect.
- **Which batch to hand over.** First expiry, first out — which is not first in,
  first out: the consignment that arrived last week may carry the earlier
  expiry. Expired lots are never allocated and are always reported.
- **What expiry has already cost you.** Write-offs added up over a look-back
  window, so the trade's "two to five percent of stock value" becomes your own
  figure.

## Who it's for

Independent pharmacy owners, chemists, botikas and small diagnostic labs in
**India and the Philippines** — anyone holding stock that dies on a date,
counting it in a spreadsheet, and finding out about a dead batch when a customer
is already at the counter. It is for the owner, not for an IT department.

## The rules it implements

**The expiry on the pack.** Most packs carry a month and a year (`EXP 03/2027`,
`EXP MAR 2027`). The settled trade convention is that such a pack is good
through the **last day** of that month, so `03/2027` is read as 31 March 2027,
`02/2028` as 29 February and `02/2027` as 28 February. A pack is saleable
through the whole of its expiry date: the expiry day itself is urgent, the day
after it is expired by one day.

**India — Drugs and Cosmetics Act, 1940 and Drugs Rules, 1945.** A drug may not
be sold, or stocked or exhibited for sale, after its labelled date of expiry,
and the conditions of a retail sale licence (Forms 20 and 21) require stock to
be stored as labelled with expired stock kept apart from saleable stock.
Confirm with the **CDSCO** and your State Drugs Control Department.

**Philippines — FDA licence to operate.** A retail drug outlet operates under a
Licence to Operate from the FDA under Republic Act No. 3720 as amended by
Republic Act No. 9711 (FDA Act of 2009), with the Philippine Pharmacy Act of
2016 (Republic Act No. 10918) governing practice; expired products must be
segregated and must not be dispensed. Confirm with **FDA Philippines** and the
Professional Regulation Commission.

**India — section 34(2), CGST Act, 2017.** A credit note against a supply cannot
be declared later than 30 November following the end of the financial year of
the supply, so a batch bought three financial years ago can be inside the
distributor's window and still be past saving. Record the purchase invoice date
and the tool uses whichever deadline falls first, and says which. The month was
September until 1 October 2022. **Indicative** — confirm with the **CBIC** or
your tax adviser. There is no Philippine equivalent in this tool, and it says so
rather than guessing.

**The return window is not a regulation.** No law in either market obliges a
distributor to take back saleable near-expiry stock, and no regulator fixes when
they will. It is a commercial term in your purchase agreement, it differs by
distributor and product line, and some lines cannot be returned at all.

### Every figure, and where it comes from

All of these live as named constants in one file,
[`internal/stock/policy.go`](internal/stock/policy.go), and all of them are
editable under **Data & settings** in the tool.

| Figure | Default | Where it comes from |
| --- | --- | --- |
| Watch tier | 90 days before expiry | Your choice of how early to be told |
| Warn tier | 60 days before expiry | Your choice |
| Urgent tier | 30 days before expiry | Your choice |
| Return window opens | 180 days before expiry | Trade practice — **indicative** |
| Return window closes | 90 days before expiry | Trade practice — **indicative** |
| Dispatch lead time | 0 days | Your logistics; no default could be honest |
| Credit rate | 100% of cost | No deduction assumed; most agreements deduct something |
| Closing-soon notice | 14 days | How loudly the screen talks |
| Loss look-back | 365 days | How far back the loss log counts |
| Financial year end (IN) | 31 March | CGST Act, 2017 |
| Credit-note cut-off (IN) | 30 November after that | Section 34(2), CGST Act, 2017 — **indicative** |

The two defaults that matter most work together: the return window closes 90
days before expiry, the same day the watch tier opens. By the time a batch
reaches the warn tier the window has already shut — which is exactly the gap a
message in a group chat cannot close.

## Install & run

```sh
git clone https://github.com/sreenivas-sadhu-prabhakara/batch-near-expiry-guardian.git
cd batch-near-expiry-guardian

go run .                                      # run it straight away
go build -o batch-near-expiry-guardian .      # or keep the single binary
./batch-near-expiry-guardian

# then open http://127.0.0.1:8080
```

Two optional flags:

```sh
./batch-near-expiry-guardian -addr 127.0.0.1:9000 -data ~/Documents/pharmacy/stock.json
```

It binds to the loopback address, so it is reachable only from the machine it
runs on. Go 1.25 to build; nothing at all to run.

## Your existing stock sheet

Import reads a CSV and insists on only three columns — a product, a batch number
and an expiry. It picks up quantity, cost, price, supplier, shelf and invoice
date under most of the names a shop actually uses (`item`, `lot no`, `exp`,
`qty`, `rate`, `mrp`, `ptr`, `rack`, `bill date`). A line missing one of the
three is skipped and reported with its line number: a guessed expiry is worse
than no record. An import **replaces** the stock list rather than merging into
it, because merging two counts of one shelf is how a count silently doubles.

## Where your data is stored

One JSON file on your machine: `./batch-near-expiry-guardian-data.json`, or
wherever `-data` points. It is written atomically (temporary file, then a
rename) so a power cut leaves the previous good file rather than half a file,
and it is created readable by its owner only, because it holds your purchase
prices and supplier terms.

Nothing leaves the machine: no telemetry, no analytics, no update check, no
external font or script, no connection to any portal or distributor system.

Taking stock off the shelf records a change in quantity and nothing else — no
customer, no prescriber, no sale. This is a stock book, not a sale register, and
it keeps nothing that would make it one.

## Scope and accuracy

This tool does arithmetic on dates and money. It is **not** a statutory
register, not a billing system, and not legal, tax or professional advice.

The tiers and the return window are **defaults, not facts**. Your distributor's
credit-note policy is the only authority for the window; get it in writing and
put the real figures in Settings before you rely on the money columns. The
statutory points above are stated as understood at the time of writing and are
marked indicative — confirm them with the CDSCO, the CBIC, FDA Philippines or
your own adviser. The credit you actually receive depends on your distributor
accepting the goods, and nothing here obliges them to.

The loss figure is measured against the cost value of stock on hand today, not
against a year of purchases, because the tool never sees your purchases. Read it
as an order of magnitude, not an audited number.

## Development

The rules live in `internal/stock` as pure functions with table-driven tests —
dates, money, the tiers, the return window, FEFO allocation, the rollups and
the CSV reader. The root package is the file store and the HTTP layer.

```sh
gofmt -l .
go vet ./...
go test ./... -count=1
go build -o /tmp/batch-near-expiry-guardian-bin .
```

MIT licensed.
