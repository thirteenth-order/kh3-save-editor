// The form half of the editor. Every control on it is built from the schema:
// nothing below names a Kingdom Hearts field, and adding one to the format
// makes it appear here with no change to this file.
//
// The reason this exists beside the JSON view rather than instead of it: an id
// typed into a number box is how somebody equips a snack where they meant a
// keyblade. A picker over the real table makes the choice, and the JSON view is
// there for the bulk edits a form is bad at.
"use strict";

// EMPTY_ENTRY says what "clear this" writes for each kind of section, because
// deleting a key from the document means "leave it alone", not "empty it".
function emptyEntry(sec, ent) {
  if ((sec.entry || []).some(function (f) { return f.kind === "equip"; })) return null;
  const out = Object.assign({}, ent);
  for (const f of sec.entry || []) {
    if (f.kind === "derived") continue;
    if (f.kind === "word") { out[f.key] = "0x00000444"; continue; } // absent
    out[f.key] = 0;
  }
  return out;
}

function fieldLabel(f) {
  const n = el("div", "fl");
  n.append(el("span", "flt", f.label || f.key));
  if (f.offset) {
    const off = el("span", "flo", f.offset);
    off.title = "stored at " + f.offset;
    n.append(off);
  }
  if (f.note) {
    const q = el("span", "flq", "?");
    q.title = f.note;
    n.append(q);
  }
  return n;
}

// control renders one field. ref is {get, set}; set is what marks the document
// dirty and re-runs validation.
function control(f, ref, ctx) {
  if (f.kind === "text") {
    const input = document.createElement("input");
    input.type = "text";
    input.className = "fin";
    input.spellcheck = false;
    if (f.maxLen) input.maxLength = f.maxLen;
    input.value = ref.get() == null ? "" : String(ref.get());
    input.disabled = !!f.readonly;
    input.oninput = function () { ref.set(input.value); ctx.edited(); };
    return input;
  }
  if (f.kind === "bool") {
    // The field's own label sits above the control, so the switch carries no
    // copy of its own: two "enabled"s stacked on top of each other is what
    // that looked like.
    const sw = optionSwitch("", asNumber(ref.get()) === 1);
    sw.input.disabled = !!f.readonly;
    sw.input.onchange = function () { ref.set(sw.input.checked ? 1 : 0); ctx.edited(); };
    return sw.node;
  }
  if (f.kind === "enum") {
    const box = combo({
      entries: table(f.table).list,
      value: asNumber(ref.get()) || 0,
      onPick: function (id) { ref.set(id); ctx.edited(); },
    });
    return box.node;
  }
  if (f.kind === "flags" || f.kind === "word") {
    return bitsControl(f, ref, ctx);
  }
  if (f.kind === "equip") {
    // An equipment id is only meaningful beside its type, and control() sees
    // one field at a time, so the slot is rendered whole by equipRow instead.
    // Reaching here means a section grew an equip field somewhere unexpected.
    return el("span", "hint err", "an equipment id needs its type beside it");
  }
  // Plain number.
  const input = document.createElement("input");
  input.type = "number";
  input.className = "fin num";
  if (f.min !== undefined) input.min = String(f.min);
  if (f.max !== undefined) input.max = String(f.max);
  input.value = String(asNumber(ref.get()) == null ? 0 : asNumber(ref.get()));
  input.disabled = !!f.readonly;
  input.oninput = function () {
    const n = Number(input.value);
    if (!Number.isInteger(n)) return;
    ref.set(n);
    ctx.edited();
  };
  return input;
}

// bitsControl is a hex field beside a checkbox per named bit. Both edit the
// same number, because the bits are the readable half and the raw word is the
// half that can hold something the names do not cover.
function bitsControl(f, ref, ctx) {
  const wrap = el("div", "bits");
  const hex = document.createElement("input");
  hex.type = "text";
  hex.className = "fin mono narrow";
  hex.spellcheck = false;

  const boxes = [];
  const width = f.kind === "word" ? 8 : 2;

  function current() { return asNumber(ref.get()) || 0; }

  function render() {
    const v = current();
    hex.value = "0x" + (v >>> 0).toString(16).toUpperCase().padStart(width, "0");
    for (const b of boxes) b.input.checked = (v & (1 << b.bit)) !== 0;
    // Everything from bit 12 up is which piece of equipment granted the
    // ability. It is reported rather than offered: the bit says the gear is
    // the source, and clearing it here would not take the gear off.
    if (f.kind === "word") {
      const src = v >>> 12;
      srcNote.textContent = src ? "granted by equipment · source " + src : "";
    }
  }

  function put(v) { ref.set("0x" + (v >>> 0).toString(16).toUpperCase().padStart(width, "0")); ctx.edited(); render(); }

  hex.oninput = function () {
    const n = asNumber(hex.value);
    if (n === null) return;
    ref.set(hex.value.trim());
    ctx.edited();
    for (const b of boxes) b.input.checked = (n & (1 << b.bit)) !== 0;
  };

  const row = el("div", "bitrow");
  for (const bit of f.bits || []) {
    const sw = el("label", "bit");
    const input = document.createElement("input");
    input.type = "checkbox";
    const mark = el("span", "bx");
    sw.append(input, mark, el("span", null, bit.label));
    if (bit.note) sw.title = bit.note;
    input.onchange = function () {
      const v = current();
      put(input.checked ? (v | (1 << bit.bit)) : (v & ~(1 << bit.bit)));
    };
    boxes.push({ bit: bit.bit, input: input });
    row.append(sw);
  }
  const srcNote = el("span", "hint tiny");
  wrap.append(hex, row, srcNote);
  render();
  return wrap;
}

/* --------------------------------------------------------------- layout -- */

function fieldRow(f, ref, ctx) {
  const row = el("div", "frow");
  row.append(fieldLabel(f), control(f, ref, ctx));
  if (f.readonly) row.classList.add("ro");
  return row;
}

function refIn(obj, key) {
  return {
    get: function () { return obj[key]; },
    set: function (v) { obj[key] = v; },
  };
}

// entryRef reaches into an entry that a dump may have written as an object or
// a bare number, and normalises it to the object form on the first edit.
function entryRef(owner, index, key) {
  return {
    get: function () {
      const ent = owner[index];
      if (ent && typeof ent === "object") return ent[key];
      return ent;
    },
    set: function (v) {
      let ent = owner[index];
      if (!ent || typeof ent !== "object") ent = {};
      ent[key] = v;
      owner[index] = ent;
    },
  };
}

function collapsible(title, sub, build, open) {
  const box = el("section", "fold");
  const head = el("button", "fold-head");
  head.type = "button";
  const chev = icon("i-chev");
  chev.setAttribute("class", "fold-chev");
  head.append(chev, el("span", "fold-title", title));
  if (sub) head.append(el("span", "fold-sub", sub));
  const body = el("div", "fold-body");
  body.hidden = true;
  let built = false;
  head.onclick = function () {
    if (!built) { body.append(build()); built = true; }
    body.hidden = !body.hidden;
    box.classList.toggle("open", !body.hidden);
  };
  box.append(head, body);
  if (open) head.onclick();
  return box;
}

// section dispatches on shape. ctx carries the edit callback and the save's
// capabilities, so a section the save does not reach renders an explanation
// instead of controls that would fail on write.
function sectionNode(sec, value, ctx) {
  if (sec.requires === "records" && !ctx.caps.records) {
    return el("p", "hint", "This save stops before the record block, so it carries no " +
      (sec.label || sec.key).toLowerCase() + ".");
  }
  switch (sec.shape) {
    case "object": return objectNode(sec, value, ctx);
    case "group": return groupNode(sec, value, ctx);
    case "index": return indexNode(sec, value, ctx);
    case "names": return namesNode(sec, value, ctx);
  }
  return el("p", "hint", "");
}

function withNote(sec, node) {
  if (!sec.note) return node;
  const wrap = el("div");
  wrap.append(el("p", "secnote", sec.note), node);
  return wrap;
}

function objectNode(sec, value, ctx) {
  const grid = el("div", "fgrid");
  for (const f of sec.fields || []) {
    if (f.kind === "derived") continue;
    if (value[f.key] === undefined) continue;
    grid.append(fieldRow(f, refIn(value, f.key), ctx));
  }
  const wrap = el("div");
  wrap.append(grid);
  for (const sub of sec.sections || []) {
    if (value[sub.key] === undefined) continue;
    wrap.append(collapsible(sub.label || sub.key, "", function () {
      return sectionNode(sub, value[sub.key], ctx);
    }));
  }
  return withNote(sec, wrap);
}

function groupNode(sec, value, ctx) {
  const wrap = el("div");
  for (const sub of sec.sections || []) {
    if (value[sub.key] === undefined && sub.requires !== "records") continue;
    wrap.append(collapsible(sub.label || sub.key, "", function () {
      return sectionNode(sub, value[sub.key] || {}, ctx);
    }));
  }
  return withNote(sec, wrap);
}

function namesNode(sec, value, ctx) {
  const wrap = el("div");
  for (const key of sec.keys) {
    if (value[key] === undefined) continue;
    wrap.append(collapsible(key, entrySummary(sec, value[key]), function () {
      return entryNode(sec, value[key], ctx);
    }));
  }
  return withNote(sec, wrap);
}

// entryNode renders one entry: its own fields, then the sections that live
// inside it. This is what makes a character card hold stats, AI, equipment and
// abilities without any of them being named here.
function entryNode(sec, ent, ctx) {
  const wrap = el("div");
  const grid = el("div", "fgrid");
  for (const f of sec.entry || []) {
    if (f.kind === "derived") continue;
    if (ent[f.key] === undefined) continue;
    grid.append(fieldRow(f, refIn(ent, f.key), ctx));
  }
  if (grid.children.length) wrap.append(grid);
  for (const sub of sec.sections || []) {
    if (ent[sub.key] === undefined) continue;
    wrap.append(collapsible(sub.label || sub.key, "", function () {
      return sectionNode(sub, ent[sub.key], ctx);
    }));
  }
  return wrap;
}

function entrySummary(sec, ent) {
  if (!ent || typeof ent !== "object") return "";
  const bits = [];
  for (const f of sec.entry || []) {
    if (f.kind === "derived" || ent[f.key] === undefined) continue;
    bits.push((f.label || f.key).toLowerCase() + " " + ent[f.key]);
    if (bits.length >= 3) break;
  }
  return bits.join(" · ");
}

/* --------------------------------------------------------- index shapes -- */

function isEquipSection(sec) {
  return (sec.entry || []).some(function (f) { return f.kind === "equip"; });
}

function indexNode(sec, value, ctx) {
  if (sec.entryKeys && sec.entryKeys.length) return pagedNode(sec, value, ctx);
  if (isEquipSection(sec)) return equipNode(sec, value, ctx);
  if (sec.sparse) return sparseNode(sec, value, ctx);
  return denseNode(sec, value, ctx);
}

// equipNode is one of the four equipment arrays. A slot is a type byte and an
// id, and the type is what says which of the ten id spaces the id lives in, so
// the two are rendered together and choosing a type re-points the id picker at
// the table that names ids of that type. This is the whole reason the schema
// publishes the type dispatch.
function equipNode(sec, value, ctx) {
  const wrap = el("div", "sparse");
  const rows = el("div", "srows");

  function slotRow(i) {
    const key = String(i);
    const ent = value[key];
    const row = el("div", "srow slot");
    const head = el("div", "sname");
    head.append(el("span", "snt", "slot " + i));
    row.append(head);

    if (ent === undefined || ent === null) {
      const fill = pill("Put something here", "i-plus", "quiet tiny");
      fill.onclick = function () {
        value[key] = { type: 0, id: 0, enabled: true };
        ctx.edited();
        paint();
      };
      row.append(fill);
      if (ent === null) row.append(el("span", "hint", "cleared on write"));
      return row;
    }

    const grid = el("div", "fgrid tight");

    const idBox = combo({
      entries: entriesFor(asNumber(ent.type) || 0),
      value: asNumber(ent.id) || 0,
      onPick: function (id) { ent.id = id; ctx.edited(); },
    });

    const typeBox = combo({
      entries: table("ItemTypes").list,
      value: asNumber(ent.type) || 0,
      onPick: function (t) {
        ent.type = t;
        idBox.setEntries(entriesFor(t));
        ctx.edited();
      },
    });

    const typeRow = el("div", "frow");
    typeRow.append(fieldLabel({ label: "type" }), typeBox.node);
    const idRow = el("div", "frow");
    idRow.append(fieldLabel({ label: "item" }), idBox.node);
    grid.append(typeRow, idRow);

    const sw = optionSwitch("", ent.enabled !== false && ent.enabled !== 0);
    sw.input.onchange = function () { ent.enabled = sw.input.checked; ctx.edited(); };
    const enRow = el("div", "frow");
    enRow.append(fieldLabel({ label: "enabled" }), sw.node);
    grid.append(enRow);
    row.append(grid);

    const drop = pill(null, "i-x", "quiet round tiny danger");
    drop.title = "empty this slot";
    drop.setAttribute("aria-label", "Empty slot " + i);
    drop.onclick = function () { value[key] = null; ctx.edited(); paint(); };
    row.append(drop);
    return row;
  }

  function paint() {
    rows.innerHTML = "";
    for (let i = 0; i < sec.count; i++) rows.append(slotRow(i));
  }
  paint();
  wrap.append(rows);
  return withNote(sec, wrap);
}

// entriesFor is the type-byte dispatch the schema publishes: an id read
// against the wrong table is how an editor hands somebody a keyblade that
// reads as a snack.
function entriesFor(type) {
  const name = (SCHEMA.equipTables || {})[String(type)];
  return name ? table(name).list : [];
}

// pagedNode is the shortcut pages: an index, each entry split into a fixed set
// of named slots.
function pagedNode(sec, value, ctx) {
  const wrap = el("div");
  for (let i = 0; i < sec.count; i++) {
    const page = value[String(i)];
    if (!page) continue;
    const box = el("div", "page");
    box.append(el("div", "pagelabel", (sec.label || sec.key) + " " + (i + 1)));
    const grid = el("div", "fgrid");
    for (const bk of sec.entryKeys) {
      if (page[bk] === undefined) continue;
      for (const f of sec.entry || []) {
        if (f.kind === "derived") continue;
        const shown = Object.assign({}, f, { label: bk });
        grid.append(fieldRow(shown, entryRef(page, bk, f.key), ctx));
      }
    }
    box.append(grid);
    wrap.append(box);
  }
  return withNote(sec, wrap);
}

// denseNode lists every index the format declares, because there are few
// enough of them that the empty ones are information too: an empty party slot
// is a fact about the save.
function denseNode(sec, value, ctx) {
  const grid = el("div", "fgrid");
  for (let i = 0; i < sec.count; i++) {
    const key = String(i);
    if (value[key] === undefined) continue;
    for (const f of sec.entry || []) {
      if (f.kind === "derived") continue;
      const label = sec.indexTable ? tableName(sec.indexTable, i) : String(i);
      const shown = Object.assign({}, f, {
        label: sec.count > 1 && (sec.entry || []).filter(function (x) { return x.kind !== "derived"; }).length > 1
          ? label + " · " + (f.label || f.key) : label,
      });
      grid.append(fieldRow(shown, entryRef(value, key, f.key), ctx));
    }
  }
  return withNote(sec, grid);
}

// sparseNode is the shape for the big arrays: the entries a save actually uses,
// filterable, with a picker for adding one that is not there yet. Painting a
// thousand inventory rows would be unusable and painting only the used ones
// with no way to add is worse, so both.
function sparseNode(sec, value, ctx) {
  const wrap = el("div", "sparse");
  const rows = el("div", "srows");

  const search = el("div", "field small");
  const q = document.createElement("input");
  q.type = "text";
  q.placeholder = "Filter " + (sec.label || sec.key).toLowerCase();
  q.setAttribute("aria-label", q.placeholder);
  search.append(icon("i-search"), q);

  const editable = (sec.entry || []).filter(function (f) { return f.kind !== "derived"; });
  const slotted = !sec.indexTable; // the index is a slot number, not an item id

  function label(i) { return sec.indexTable ? tableName(sec.indexTable, i) : "slot " + i; }

  function paint() {
    rows.innerHTML = "";
    const filter = q.value.trim().toLowerCase();
    const keys = Object.keys(value).sort(function (a, b) { return Number(a) - Number(b); });
    let shown = 0;
    for (const key of keys) {
      const name = label(Number(key));
      if (filter && name.toLowerCase().indexOf(filter) < 0 && key.indexOf(filter) < 0) continue;
      const row = el("div", "srow");
      const head = el("div", "sname");
      head.append(el("span", "snt", name), el("span", "sni", key));
      row.append(head);
      const grid = el("div", "fgrid tight");
      for (const f of editable) {
        if (value[key] === null && f.kind !== "equip") continue;
        grid.append(fieldRow(f, entryRef(value, key, f.key), ctx));
      }
      if (value[key] === null) grid.append(el("p", "hint", "cleared"));
      row.append(grid);
      const drop = pill(null, "i-x", "quiet round tiny danger");
      drop.title = "clear this in the save";
      drop.setAttribute("aria-label", "Clear " + name);
      drop.onclick = function () {
        value[key] = emptyEntry(sec, value[key]);
        ctx.edited();
        paint();
      };
      row.append(drop);
      rows.append(row);
      shown++;
    }
    if (!shown) rows.append(el("p", "hint", Object.keys(value).length
      ? "nothing matches that filter"
      : "this save has none"));
  }
  q.oninput = paint;

  const adder = el("div", "adder");
  if (slotted) {
    const free = pill("Fill the next free slot", "i-plus", "quiet");
    free.onclick = function () {
      for (let i = 0; i < sec.count; i++) {
        if (value[String(i)] !== undefined && value[String(i)] !== null) continue;
        value[String(i)] = defaultEntry(sec);
        ctx.edited();
        paint();
        return;
      }
      toast("Every slot is full", "bad");
    };
    adder.append(free);
  } else {
    const box = combo({
      entries: table(sec.indexTable).list.filter(function (e) { return e.id < sec.count; }),
      value: null,
      clearAfterPick: true,
      placeholder: "Add one that is not here yet",
      onPick: function (id) {
        if (value[String(id)] !== undefined) { toast("That one is already here"); return; }
        value[String(id)] = defaultEntry(sec);
        ctx.edited();
        paint();
      },
    });
    adder.append(box.node);
  }

  paint();
  wrap.append(search, rows, adder);
  return withNote(sec, wrap);
}

// defaultEntry is what adding a row writes: one of the thing, present and
// unseen where the format has a flag for it.
function defaultEntry(sec) {
  const ent = {};
  for (const f of sec.entry || []) {
    if (f.kind === "derived") continue;
    if (f.kind === "equip") { ent[f.key] = 0; continue; }
    if (f.kind === "word") { ent[f.key] = "0x0000044B"; continue; }
    if (f.key === "flags") { ent[f.key] = "0x03"; continue; }
    if (f.key === "type") { ent[f.key] = 0; continue; }
    if (f.kind === "bool") { ent[f.key] = 1; continue; }
    ent[f.key] = 1;
  }
  return ent;
}

/* ----------------------------------------------------------- the editor -- */

// buildForm renders the whole document. Sections are collapsed by default and
// build their bodies on first open: a full dump is sixteen characters of five
// hundred abilities each, and rendering all of it up front would cost a second
// of layout for the sake of controls nobody has scrolled to.
function buildForm(state) {
  const wrap = el("div", "form");
  const ctx = { edited: state.edited, caps: state.caps };
  for (const sec of SCHEMA.sections) {
    const value = state.doc[sec.key];
    if (value === undefined) continue;
    const count = value && typeof value === "object" ? Object.keys(value).length : 0;
    wrap.append(collapsible(sec.label || sec.key,
      count ? count + (count === 1 ? " entry" : " entries") : "",
      function () { return sectionNode(sec, value, ctx); },
      sec.key === "header"));
  }
  return wrap;
}
