/* Batch & Near-Expiry Guardian — the local interface.
   Talks only to /api on this same loopback address. No other origin is
   contacted, and every figure on screen is worked out by the binary. */
(function () {
  "use strict";

  var el = function (id) { return document.getElementById(id); };
  var state = null;
  var board = null;

  /* ───────── theme ───────── */
  (function theme() {
    var root = document.documentElement;
    var button = document.querySelector("[data-themer]");
    if (!button) { return; }
    function stored() { try { return localStorage.getItem("omsme-theme"); } catch (e) { return null; } }
    function current() {
      var set = root.getAttribute("data-theme");
      if (set) { return set; }
      return (window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches) ? "dark" : "light";
    }
    function paint() {
      var dark = current() === "dark";
      button.textContent = dark ? "☀ Light" : "☾ Dark";
      button.setAttribute("aria-label", dark ? "Switch to light theme" : "Switch to dark theme");
    }
    paint();
    button.addEventListener("click", function () {
      var next = current() === "dark" ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("omsme-theme", next); } catch (e) { /* private window */ }
      paint();
    });
    if (window.matchMedia) {
      window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", function () {
        if (!stored()) { paint(); }
      });
    }
  }());

  /* ───────── tabs ───────── */
  var tabs = Array.prototype.slice.call(document.querySelectorAll("[data-tab]"));
  tabs.forEach(function (button) {
    button.addEventListener("click", function () { show(button.getAttribute("data-tab")); });
  });
  function show(name) {
    tabs.forEach(function (button) {
      button.setAttribute("aria-pressed", button.getAttribute("data-tab") === name ? "true" : "false");
    });
    ["shelf", "returns", "batches", "pick", "settings"].forEach(function (panel) {
      el("panel-" + panel).hidden = panel !== name;
    });
    if (name === "returns") { loadReturns(); }
  }

  /* ───────── plumbing ───────── */
  function flash(message, level) {
    var box = el("flash");
    box.textContent = message;
    box.hidden = !message;
    if (level) { box.setAttribute("data-level", level); } else { box.removeAttribute("data-level"); }
  }

  function api(method, path, body) {
    var options = { method: method, headers: { "Accept": "application/json" } };
    if (body !== undefined) {
      options.headers["Content-Type"] = "application/json";
      options.body = JSON.stringify(body);
    }
    return fetch(path, options).then(function (response) {
      return response.text().then(function (text) {
        var parsed = null;
        try { parsed = text ? JSON.parse(text) : {}; } catch (e) { parsed = { error: text }; }
        if (!response.ok) { throw new Error((parsed && parsed.error) || ("the request failed: " + response.status)); }
        return parsed;
      });
    });
  }

  function text(node, value) { node.textContent = value === undefined || value === null ? "" : String(value); }

  function cell(row, value, className) {
    var td = document.createElement("td");
    if (className) { td.className = className; }
    if (value instanceof Node) { td.appendChild(value); } else { text(td, value); }
    row.appendChild(td);
    return td;
  }

  function chip(label, band) {
    var span = document.createElement("span");
    span.className = "chip " + band;
    text(span, label);
    return span;
  }

  function stack(main, meta) {
    var box = document.createElement("div");
    var strong = document.createElement("span");
    strong.className = "prod";
    text(strong, main);
    box.appendChild(strong);
    if (meta) {
      var small = document.createElement("span");
      small.className = "meta";
      text(small, meta);
      box.appendChild(small);
    }
    return box;
  }

  function button(label, className, onClick) {
    var node = document.createElement("button");
    node.type = "button";
    if (className) { node.className = className; }
    text(node, label);
    node.addEventListener("click", onClick);
    return node;
  }

  function plural(n, one, many) { return n + " " + (n === 1 ? one : many); }

  function asOf() { return el("asof").value || ""; }
  function query(extra) {
    var parts = [];
    if (asOf()) { parts.push("asOf=" + encodeURIComponent(asOf())); }
    (extra || []).forEach(function (pair) { parts.push(pair); });
    return parts.length ? "?" + parts.join("&") : "";
  }

  /* ───────── state ───────── */
  function loadState() {
    return api("GET", "/api/state").then(function (data) {
      state = data;
      text(el("data-file"), shortPath(data.dataFile));
      el("data-file").title = data.dataFile;
      text(el("data-path"), data.dataFile);
      el("export-link").href = "/api/export.csv" + query();
      text(el("cost-sign"), data.symbol);
      text(el("price-sign"), data.symbol);
      if (!el("asof").value) { el("asof").value = data.today; }
      fillMarkets();
      fillPolicy();
      fillSuppliers();
      fillProducts();
      fillAuthorities();
      renderBatches();
      text(el("defaults-note"),
        "The defaults are " + data.defaults.watchDays + "/" + data.defaults.warnDays + "/" +
        data.defaults.urgentDays + " day tiers, a return window from " + data.defaults.returnOpensDays +
        " to " + data.defaults.returnClosesDays + " days before expiry, and " + data.defaults.creditPercent +
        "% credit. They are the commonest terms in the trade, not a rule: confirm yours with your distributor in writing.");
      var reg = data.policy.market === "PH"
        ? "FDA Philippines licence to operate"
        : "Drugs and Cosmetics Act and Rules";
      text(el("reg-pill"), reg);
    });
  }

  function shortPath(path) {
    if (!path) { return ""; }
    var parts = path.split(/[\\/]/);
    return parts[parts.length - 1];
  }

  function fillMarkets() {
    var select = el("pol-market");
    select.innerHTML = "";
    state.markets.forEach(function (market) {
      var option = document.createElement("option");
      option.value = market.code;
      text(option, market.label);
      select.appendChild(option);
    });
    select.value = state.policy.market;
  }

  function fillPolicy() {
    var p = state.policy;
    el("pol-watch").value = p.watchDays;
    el("pol-warn").value = p.warnDays;
    el("pol-urgent").value = p.urgentDays;
    el("pol-opens").value = p.returnOpensDays;
    el("pol-closes").value = p.returnClosesDays;
    el("pol-lead").value = p.returnLeadDays;
    el("pol-credit").value = p.creditPercent;
    el("pol-soon").value = p.closingSoonDays;
    el("pol-loss").value = p.lossWindowDays;
    el("pol-cutoff").checked = !!p.applyCreditNoteCutoff;
    el("cutoff-line").hidden = p.market !== "IN";
  }

  function fillSuppliers() {
    var list = el("supplier-list");
    list.innerHTML = "";
    state.suppliers.forEach(function (name) {
      var option = document.createElement("option");
      option.value = name;
      list.appendChild(option);
    });
  }

  function fillProducts() {
    var select = el("pick-product");
    var previous = select.value;
    select.innerHTML = "";
    var blank = document.createElement("option");
    blank.value = "";
    text(blank, state.products.length ? "Choose a product" : "No products on file yet");
    select.appendChild(blank);
    state.products.forEach(function (name) {
      var option = document.createElement("option");
      option.value = name;
      text(option, name);
      select.appendChild(option);
    });
    if (previous && state.products.indexOf(previous) >= 0) { select.value = previous; }
  }

  function fillAuthorities() {
    var box = el("authorities");
    box.innerHTML = "";
    state.authorities.forEach(function (item) {
      var card = document.createElement("div");
      card.className = "auth";
      var scope = document.createElement("div");
      scope.className = "scope";
      text(scope, item.scope === "IN" ? "India" : item.scope === "PH" ? "Philippines" : "Both markets");
      card.appendChild(scope);
      var point = document.createElement("p");
      text(point, item.point);
      card.appendChild(point);
      var source = document.createElement("div");
      source.className = "src";
      text(source, item.source + " · confirm with " + item.confirm);
      card.appendChild(source);
      box.appendChild(card);
    });
  }

  /* ───────── shelf ───────── */
  function loadBoard() {
    return api("GET", "/api/board" + query()).then(function (data) {
      board = data;
      renderBoard();
      renderLoss();
      el("export-link").href = "/api/export.csv" + query();
    });
  }

  function renderBoard() {
    var r = board.returns;
    text(el("hl-atrisk"), board.atRisk.cost.text);
    text(el("hl-atrisk-sub"), board.atRisk.batches
      ? plural(board.atRisk.batches, "batch", "batches") + " · " + plural(board.atRisk.units, "unit", "units") +
        " · " + board.atRisk.retail.text + " at retail · out of " + board.totals.cost.text + " of stock on hand"
      : "No batch is inside an alert tier on this date.");

    text(el("hl-open"), r.recover.text);
    text(el("hl-open-sub"), r.openBatches
      ? plural(r.openBatches, "batch", "batches") + " worth " + r.open.text + " at cost" +
        (r.nextDeadlinePretty ? " · first deadline " + r.nextDeadlinePretty : "")
      : "Nothing is inside a return window.");

    text(el("hl-stranded"), r.stranded.text);
    text(el("hl-stranded-sub"), r.strandedBatches
      ? plural(r.strandedBatches, "batch", "batches") + " past the window but still saleable"
      : "Nothing has missed its window.");

    text(el("hl-dead"), r.dead.text);
    text(el("hl-dead-sub"), r.deadBatches
      ? plural(r.deadBatches, "batch", "batches") + " · " + plural(r.deadUnits, "unit", "units") + " to segregate"
      : "No expired stock on the shelf.");

    var counts = el("tier-counts");
    counts.innerHTML = "";
    board.buckets.forEach(function (bucket) {
      var card = document.createElement("div");
      card.className = "c " + bucket.band;
      var n = document.createElement("div");
      n.className = "n";
      text(n, bucket.batches);
      var label = document.createElement("div");
      label.className = "l";
      text(label, bucket.label);
      var value = document.createElement("div");
      value.className = "v";
      text(value, bucket.cost.text);
      card.appendChild(n);
      card.appendChild(label);
      card.appendChild(value);
      counts.appendChild(card);
    });

    var body = el("shelf-rows");
    body.innerHTML = "";
    board.rows.forEach(function (row) {
      var tr = document.createElement("tr");
      if (row.verdict.tier === "expired") { tr.className = "is-dead"; }
      cell(tr, chip(row.tierLabel, row.verdict.band));
      cell(tr, stack(row.batch.product, "batch " + row.batch.batchNo + (row.batch.shelf ? " · shelf " + row.batch.shelf : "")));
      cell(tr, stack(row.expiry, row.says));
      cell(tr, row.batch.qty, "num");
      cell(tr, row.cost.text, "num");
      cell(tr, stack(row.stateLabel, row.verdict.return.deadline
        ? "deadline " + fmtDate(row.verdict.return.deadline)
        : ""));
      cell(tr, row.action, "act");
      body.appendChild(tr);
    });
    el("shelf-empty").hidden = board.rows.length > 0;

    var p = state.policy;
    text(el("tier-note"),
      "Tiers are " + p.watchDays + ", " + p.warnDays + " and " + p.urgentDays +
      " days before expiry. A pack is saleable through the whole of its expiry date, so the expiry day itself still shows as urgent rather than expired. " +
      "With a window that closes " + p.returnClosesDays + " days before expiry, anything in the warn tier or later has already missed it. " +
      state.expiryNote);
  }

  function renderLoss() {
    var loss = board.loss;
    text(el("loss-cost"), loss.cost.text);
    text(el("loss-sub"), plural(loss.batches, "batch", "batches") + " · " + plural(loss.units, "unit", "units") +
      " · " + loss.share + " of stock on hand · since " + loss.sincePretty);
    text(el("loss-returned"), loss.returned.text);
    text(el("loss-returned-sub"), plural(loss.returnedBatches, "batch", "batches") +
      " sent back instead of thrown away");
  }

  function fmtDate(iso) {
    if (!iso) { return ""; }
    var parts = String(iso).split("-");
    if (parts.length !== 3) { return iso; }
    return parts[2] + "-" + parts[1] + "-" + parts[0];
  }

  /* ───────── returns ───────── */
  function loadReturns() {
    return api("GET", "/api/returns" + query()).then(function (data) {
      text(el("ret-recover"), data.recover.text);
      text(el("ret-sub"), data.rows.length
        ? plural(data.rows.length, "batch", "batches") + " worth " + data.total.text + " at cost"
        : "Nothing to send back on this date.");
      text(el("returns-note"), data.note);

      var body = el("returns-rows");
      body.innerHTML = "";
      data.rows.forEach(function (row) {
        var tr = document.createElement("tr");
        var days = row.verdict.return.daysToDeadline;
        cell(tr, stack(fmtDate(row.verdict.return.deadline),
          row.verdict.return.state === "open"
            ? (days === 0 ? "today" : plural(days, "day", "days") + " left")
            : "opens " + fmtDate(row.verdict.return.opensOn)), "deadline");
        cell(tr, stack(row.batch.product, "batch " + row.batch.batchNo + " · expires " + row.expiry));
        cell(tr, row.batch.supplier || "—");
        cell(tr, row.batch.qty, "num");
        cell(tr, row.cost.text, "num");
        cell(tr, row.recoverable.text, "num");
        cell(tr, row.verdict.return.limitedBy === "creditNote" ? "credit note" : "window");
        var actions = document.createElement("div");
        actions.className = "rowbtns";
        actions.appendChild(button("Sent back", "go", function () { settle(row.batch.id, "returned"); }));
        cell(tr, actions);
        body.appendChild(tr);
      });
      el("returns-empty").hidden = data.rows.length > 0;
    }).catch(fail);
  }

  /* ───────── batches ───────── */
  function renderBatches() {
    var body = el("batch-rows");
    body.innerHTML = "";
    var rows = state.batches.slice().sort(function (a, b) {
      if (a.expiresOn !== b.expiresOn) { return a.expiresOn < b.expiresOn ? -1 : 1; }
      return a.product.toLowerCase() < b.product.toLowerCase() ? -1 : 1;
    });
    rows.forEach(function (batch) {
      var tr = document.createElement("tr");
      var settled = batch.writtenOff || batch.returnedOn;
      if (settled) { tr.className = "is-settled"; }
      cell(tr, stack(batch.product, "batch " + batch.batchNo + (batch.pack ? " · " + batch.pack : "")));
      cell(tr, fmtDate(batch.expiresOn));
      cell(tr, batch.qty, "num");
      cell(tr, money(batch.unitCost * batch.qty), "num");
      cell(tr, batch.supplier || "—");
      cell(tr, batch.writtenOff
        ? "written off " + fmtDate(batch.writtenOffOn)
        : batch.returnedOn ? "returned " + fmtDate(batch.returnedOn) : "on shelf");

      var actions = document.createElement("div");
      actions.className = "rowbtns";
      actions.appendChild(button("Edit", "", function () { editBatch(batch); }));
      if (settled) {
        actions.appendChild(button("Undo", "", function () { settle(batch.id, "reopen"); }));
      } else {
        actions.appendChild(button("Sent back", "go", function () { settle(batch.id, "returned"); }));
        actions.appendChild(button("Write off", "danger", function () { settle(batch.id, "writeOff"); }));
      }
      actions.appendChild(button("Delete", "danger", function () { removeBatch(batch); }));
      cell(tr, actions);
      body.appendChild(tr);
    });
    el("batches-empty").hidden = rows.length > 0;
  }

  /* money() formats a minor-unit amount for the one place the interface has to
     do it itself: the batch list, which is built from the stored records
     rather than from a graded board. Everything else uses the text the binary
     sends. */
  function money(minor) {
    var amount = (minor || 0) / 100;
    var locale = state.currency === "PHP" ? "en-PH" : "en-IN";
    try {
      return state.symbol + amount.toLocaleString(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
    } catch (e) {
      return state.symbol + amount.toFixed(2);
    }
  }

  function editBatch(batch) {
    el("batch-id").value = batch.id;
    el("batch-product").value = batch.product;
    el("batch-pack").value = batch.pack;
    el("batch-no").value = batch.batchNo;
    el("batch-expiry").value = batch.expiresOn;
    el("batch-purchased").value = batch.purchasedOn || "";
    el("batch-qty").value = batch.qty;
    el("batch-cost").value = (batch.unitCost / 100).toFixed(2);
    el("batch-price").value = (batch.unitPrice / 100).toFixed(2);
    el("batch-supplier").value = batch.supplier;
    el("batch-shelf").value = batch.shelf;
    el("batch-note").value = batch.note;
    text(el("batch-form-heading"), "Edit batch " + batch.batchNo);
    text(el("batch-save"), "Save changes");
    el("batch-cancel").hidden = false;
    readExpiry();
    show("batches");
    el("batch-product").focus();
  }

  function resetForm() {
    el("batch-form").reset();
    el("batch-id").value = "";
    el("batch-qty").value = "0";
    text(el("batch-form-heading"), "Add a batch");
    text(el("batch-save"), "Save batch");
    el("batch-cancel").hidden = true;
    text(el("expiry-reading"), "");
    el("expiry-reading").removeAttribute("data-ok");
  }

  el("batch-cancel").addEventListener("click", resetForm);

  el("batch-form").addEventListener("submit", function (event) {
    event.preventDefault();
    var payload = {
      id: el("batch-id").value,
      product: el("batch-product").value,
      pack: el("batch-pack").value,
      batchNo: el("batch-no").value,
      expiresOn: el("batch-expiry").value,
      purchasedOn: el("batch-purchased").value,
      qty: parseInt(el("batch-qty").value, 10) || 0,
      unitCost: el("batch-cost").value,
      unitPrice: el("batch-price").value,
      supplier: el("batch-supplier").value,
      shelf: el("batch-shelf").value,
      note: el("batch-note").value
    };
    api("PUT", "/api/batch", payload).then(function (data) {
      var note = "Saved batch " + data.batch.batchNo + ", expiring " + fmtDate(data.batch.expiresOn) + ".";
      if (data.warnings && data.warnings.length) {
        flash(note + " " + data.warnings.join(" "), "warn");
      } else {
        flash(note, "ok");
      }
      resetForm();
      return refresh();
    }).catch(fail);
  });

  function removeBatch(batch) {
    if (!window.confirm("Delete batch " + batch.batchNo + " of " + batch.product + "? Use Write off instead if the stock really expired, so the loss stays countable.")) { return; }
    api("DELETE", "/api/batch?id=" + encodeURIComponent(batch.id)).then(function () {
      flash("Deleted batch " + batch.batchNo + ".", "ok");
      return refresh();
    }).catch(fail);
  }

  function settle(id, action) {
    api("POST", "/api/batch/settle", { id: id, action: action, on: asOf() }).then(function () {
      flash(action === "returned" ? "Marked as sent back to the distributor."
        : action === "writeOff" ? "Written off. It now counts in what expiry has cost you."
        : "Put back on the shelf.", "ok");
      return refresh();
    }).catch(fail);
  }

  /* ───────── expiry reading ───────── */
  var readTimer = null;
  function readExpiry() {
    var raw = el("batch-expiry").value.trim();
    var box = el("expiry-reading");
    if (!raw) {
      text(box, "");
      box.removeAttribute("data-ok");
      return;
    }
    api("GET", "/api/read-expiry?v=" + encodeURIComponent(raw)).then(function (data) {
      if (data.ok) {
        box.setAttribute("data-ok", "yes");
        text(box, "read as " + data.pretty + " (" + data.month + ")");
      } else {
        box.setAttribute("data-ok", "no");
        text(box, data.message);
      }
    }).catch(function () { text(box, ""); });
  }
  el("batch-expiry").addEventListener("input", function () {
    window.clearTimeout(readTimer);
    readTimer = window.setTimeout(readExpiry, 200);
  });

  /* ───────── pick ───────── */
  el("pick-form").addEventListener("submit", function (event) {
    event.preventDefault();
    var product = el("pick-product").value;
    var qty = parseInt(el("pick-qty").value, 10) || 0;
    if (!product) { flash("Choose a product first.", "bad"); return; }
    api("GET", "/api/pick?product=" + encodeURIComponent(product) + "&qty=" + qty +
      (asOf() ? "&asOf=" + encodeURIComponent(asOf()) : "")).then(function (data) {
      renderPick(data);
      flash("", null);
    }).catch(fail);
  });

  function renderPick(list) {
    el("pick-result").hidden = false;
    text(el("pick-head"), list.shortfall
      ? plural(list.allocated, "unit", "units") + " of " + list.requested
      : plural(list.allocated, "unit", "units"));
    text(el("pick-sub"), (list.shortfall
      ? "Short by " + plural(list.shortfall, "unit", "units") + ". "
      : "The whole request can be filled. ") +
      plural(list.available, "unit", "units") + " saleable on hand · " + list.value.text + " at cost · " + list.note);

    var body = el("pick-rows");
    body.innerHTML = "";
    list.lines.forEach(function (line, index) {
      var tr = document.createElement("tr");
      cell(tr, index + 1);
      cell(tr, stack("batch " + line.batchNo, line.tier === "fresh" ? "" : line.tier + " · " + plural(line.daysLeft, "day", "days") + " left"));
      cell(tr, line.shelf || "—");
      cell(tr, fmtDate(line.expiresOn));
      cell(tr, line.take, "num");
      cell(tr, line.remaining, "num");
      body.appendChild(tr);
    });

    var skipped = el("pick-skipped");
    skipped.innerHTML = "";
    list.skipped.forEach(function (item) {
      var li = document.createElement("li");
      var where = document.createElement("span");
      where.className = "where";
      text(where, "batch " + item.batchNo + " (" + plural(item.qty, "unit", "units") + "): ");
      li.appendChild(where);
      li.appendChild(document.createTextNode(item.reason));
      skipped.appendChild(li);
    });
    el("skipped-heading").hidden = list.skipped.length === 0;
    el("pick-apply").disabled = list.lines.length === 0;
    el("pick-apply").setAttribute("data-product", list.product);
    el("pick-apply").setAttribute("data-qty", String(list.allocated));
  }

  el("pick-apply").addEventListener("click", function () {
    var product = el("pick-apply").getAttribute("data-product");
    var qty = parseInt(el("pick-apply").getAttribute("data-qty"), 10) || 0;
    if (!product || qty <= 0) { return; }
    if (!window.confirm("Take " + plural(qty, "unit", "units") + " of " + product + " off the shelf?")) { return; }
    api("POST", "/api/pick/apply", { product: product, qty: qty, asOf: asOf() }).then(function () {
      flash("Taken off the shelf. Quantities updated.", "ok");
      el("pick-result").hidden = true;
      return refresh();
    }).catch(fail);
  });

  el("pick-product").addEventListener("change", function () {
    el("pick-result").hidden = true;
    loadShelfOrder();
  });

  function loadShelfOrder() {
    var product = el("pick-product").value;
    var body = el("shelforder-rows");
    body.innerHTML = "";
    if (!product) {
      el("shelforder-empty").hidden = false;
      return;
    }
    api("GET", "/api/shelf?product=" + encodeURIComponent(product) +
      (asOf() ? "&asOf=" + encodeURIComponent(asOf()) : "")).then(function (data) {
      data.rows.forEach(function (row, index) {
        var tr = document.createElement("tr");
        if (row.verdict.tier === "expired") { tr.className = "is-dead"; }
        cell(tr, index + 1);
        cell(tr, stack("batch " + row.batch.batchNo, row.batch.shelf ? "shelf " + row.batch.shelf : ""));
        cell(tr, stack(row.expiry, row.says));
        cell(tr, row.batch.qty, "num");
        cell(tr, chip(row.tierLabel, row.verdict.band));
        cell(tr, row.action, "act");
        body.appendChild(tr);
      });
      el("shelforder-empty").hidden = data.rows.length > 0;
    }).catch(fail);
  }

  /* ───────── settings ───────── */
  el("pol-market").addEventListener("change", function () {
    el("cutoff-line").hidden = el("pol-market").value !== "IN";
  });

  el("policy-form").addEventListener("submit", function (event) {
    event.preventDefault();
    var payload = {
      market: el("pol-market").value,
      watchDays: num("pol-watch"),
      warnDays: num("pol-warn"),
      urgentDays: num("pol-urgent"),
      returnOpensDays: num("pol-opens"),
      returnClosesDays: num("pol-closes"),
      returnLeadDays: num("pol-lead"),
      creditPercent: num("pol-credit"),
      closingSoonDays: num("pol-soon"),
      lossWindowDays: num("pol-loss"),
      applyCreditNoteCutoff: el("pol-cutoff").checked
    };
    api("PUT", "/api/policy", payload).then(function () {
      flash("Saved. Every deadline on the board has been worked out again from your figures.", "ok");
      return refresh();
    }).catch(fail);
  });

  function num(id) { return parseInt(el(id).value, 10) || 0; }

  el("policy-reset").addEventListener("click", function () {
    var d = state.defaults;
    el("pol-watch").value = d.watchDays;
    el("pol-warn").value = d.warnDays;
    el("pol-urgent").value = d.urgentDays;
    el("pol-opens").value = d.returnOpensDays;
    el("pol-closes").value = d.returnClosesDays;
    el("pol-lead").value = d.returnLeadDays;
    el("pol-credit").value = d.creditPercent;
    el("pol-soon").value = d.closingSoonDays;
    el("pol-loss").value = d.lossWindowDays;
    flash("The defaults are in the boxes. They are not saved until you press Save.", null);
  });

  el("import-run").addEventListener("click", function () {
    var picker = el("import-file");
    if (!picker.files || !picker.files.length) {
      flash("Choose a CSV file first.", "bad");
      return;
    }
    var file = picker.files[0];
    if (!window.confirm("This replaces your whole stock list with the " + file.name + " file. Carry on?")) { return; }
    file.text().then(function (body) {
      return fetch("/api/import.csv", {
        method: "POST",
        headers: { "Content-Type": "text/csv" },
        body: body
      }).then(function (response) {
        return response.json().then(function (data) {
          if (!response.ok) { throw new Error(data.error || "the import failed"); }
          return data;
        });
      });
    }).then(function (data) {
      el("import-report").hidden = false;
      text(el("import-summary"),
        "Read " + plural(data.read, "line", "lines") + " and stored " + plural(data.imported, "batch", "batches") +
        ". Columns matched: " + (data.columns || []).join(", ") + ".");
      var list = el("import-skipped");
      list.innerHTML = "";
      (data.skipped || []).forEach(function (problem) {
        var li = document.createElement("li");
        var where = document.createElement("span");
        where.className = "where";
        text(where, "line " + problem.line + ": ");
        li.appendChild(where);
        li.appendChild(document.createTextNode(problem.message));
        list.appendChild(li);
      });
      flash("Stock list replaced: " + plural(data.imported, "batch", "batches") + " on file.", "ok");
      picker.value = "";
      return refresh();
    }).catch(fail);
  });

  /* ───────── wiring ───────── */
  el("asof").addEventListener("change", function () {
    refresh().then(function () { flash("", null); });
  });

  function refresh() {
    return loadState().then(loadBoard).then(function () {
      if (!el("panel-returns").hidden) { return loadReturns(); }
      if (!el("panel-pick").hidden) { return loadShelfOrder(); }
      return null;
    });
  }

  function fail(err) { flash(err && err.message ? err.message : String(err), "bad"); }

  refresh().catch(fail);
}());
