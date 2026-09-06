// kh3save UI. No framework and no build step: the page is a few DOM calls
// against the local API described in server.go, split across four assets --
// ui.js for the primitives, editor.js for the JSON editor, schema.js and
// forms.js for the schema-driven half, and this file for the shell.
"use strict";

// Difficulty is the one axis the whole page turns on, so name, color and
// sigil live together and everything else indexes into this.
// A save with a difficulty byte outside 0-3 is corrupt or from a future
// format version. Fall back rather than throwing, so one bad slot cannot stop
// the whole page rendering.
const UNKNOWN_DIFF = { name: "unknown", hue: "var(--dim)", sig: "sig0" };
function diffMeta(i) { return DIFFS[i] || UNKNOWN_DIFF; }

const DIFFS = [
  { name: "Beginner", hue: "var(--beginner)", sig: "sig0" },
  { name: "Standard", hue: "var(--standard)", sig: "sig1" },
  { name: "Proud",    hue: "var(--proud)",    sig: "sig2" },
  { name: "Critical", hue: "var(--critical)", sig: "sig3" },
];

// Critical is the only difficulty that changes anything outside the flag.
const CRITICAL = 3;

const app = document.getElementById("app");
const topbar = document.getElementById("topbar");

for (const img of document.querySelectorAll("img.mark")) img.src = EMBLEM;

/* ---------------------------------------------------- segmented control -- */

function segment(currentIndex, onPick) {
  const seg = el("div", "seg");
  seg.setAttribute("role", "group");
  seg.setAttribute("aria-label", "Difficulty");
  seg.append(el("span", "seg-thumb"));

  let picked = currentIndex;
  const buttons = DIFFS.map(function (entry, i) {
    const b = el("button");
    b.type = "button";
    b.append(icon(entry.sig), el("span", null, entry.name));
    b.onclick = function () { picked = i; paint(); onPick(picked); };
    seg.append(b);
    return b;
  });

  // Arrow keys move within the group, which is what a segmented control is
  // expected to do and what makes it usable without a mouse.
  seg.onkeydown = function (e) {
    const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    picked = (picked + step + DIFFS.length) % DIFFS.length;
    paint();
    buttons[picked].focus();
    onPick(picked);
  };

  function paint() {
    seg.style.setProperty("--at", picked);
    seg.style.setProperty("--pick", DIFFS[picked].hue);
    buttons.forEach(function (b, i) {
      b.setAttribute("aria-pressed", i === picked ? "true" : "false");
      b.setAttribute("data-now", i === currentIndex ? "true" : "false");
      b.tabIndex = i === picked ? 0 : -1;
      b.title = i === currentIndex ? DIFFS[i].name + " (current setting)" : DIFFS[i].name;
    });
  }
  paint();

  return { node: seg, repaint: function (now) { currentIndex = now; picked = now; paint(); } };
}

/* ---------------------------------------------------------------- slots -- */

function slotCard(slot, index) {
  const box = el("div", "slot");
  const head = el("div", "slot-head");
  head.append(el("div", "idx", String(index + 1).padStart(2, "0")));

  if (slot.error) {
    const bad = el("div", "cur");
    bad.style.setProperty("--c", "var(--critical)");
    bad.append(icon("i-alert"), el("span", null, slot.slot));
    head.append(bad);
    box.style.setProperty("--c", "var(--critical)");
    box.append(head);
    const facts = el("div", "facts");
    facts.append(chip("i-alert", slot.error));
    box.append(facts);
    return box;
  }

  if (slot.slot === "system") {
    const sys = el("div", "cur");
    sys.style.setProperty("--c", "var(--dim)");
    sys.append(icon("i-slider"), el("span", null, "system"));
    head.append(sys);
    box.append(head);
    const facts = el("div", "facts");
    facts.append(chip("i-stack", "shared config"), chip(null, "no difficulty"));
    box.append(facts);
    return box;
  }

  const tone = diffMeta(slot.difficulty).hue;
  box.style.setProperty("--c", tone);

  const cur = el("div", "cur");
  cur.style.setProperty("--c", tone);
  const curIcon = icon(diffMeta(slot.difficulty).sig);
  cur.append(curIcon, el("span", null, slot.difficultyName));
  head.append(cur, el("div", "grow"));
  if (slot.path.indexOf(".zip!") > -1) head.append(chip("i-archive", "in zip", "hue archive"));
  head.append(chip(null, slot.slot, "mono"));
  box.append(head);

  const facts = el("div", "facts");
  facts.append(
    chip("i-level", "level " + slot.level),
    chip("i-clock", slot.playtime),
    chip("i-coin", slot.munny + " munny"));
  // A save that has not reached a named map yet would otherwise render an
  // empty pill with nothing but the pin in it.
  if (slot.location) facts.append(chip("i-pin", slot.location, "mono"));
  box.append(facts);

  const apply = pill("Apply", "i-check", "go");
  const hint = el("span", "hint");
  const out = el("div");

  // The Soldier's Earring is the one thing Critical starts with that the
  // difficulty flag does not carry, so it is the only choice a swap has. Both
  // switches live in the save they act on, appear only when they would change
  // something, and start in the state that matches what the game would have
  // done: Critical hands the earring out, leaving Critical takes it back.
  const extras = el("div", "extras");
  const grant = optionSwitch(
    "Add the Soldier&rsquo;s Earring that Critical starts with (+6&nbsp;AP)", true);
  const revoke = optionSwitch(
    "Take back the unequipped Soldier&rsquo;s Earring", true);
  extras.append(grant.node, revoke.node);
  grant.input.onchange = refresh;
  revoke.input.onchange = refresh;

  let picked = slot.difficulty;
  let showGrant = false;
  let showRevoke = false;
  const seg = segment(slot.difficulty, function (next) { picked = next; refresh(); });

  function refresh() {
    // Granting is only ever offered where it would add an earring that is not
    // there; revoking only where there is exactly the starting one to take.
    showGrant = picked === CRITICAL && slot.canGrantStartItem;
    showRevoke = slot.difficulty === CRITICAL && picked !== CRITICAL && slot.canRevokeStartItem;
    grant.node.hidden = !showGrant;
    revoke.node.hidden = !showRevoke;
    extras.hidden = !showGrant && !showRevoke;

    const itemOnly = picked === slot.difficulty && showGrant && grant.input.checked;
    apply.disabled = picked === slot.difficulty && !itemOnly;

    hint.innerHTML = "";
    if (picked !== slot.difficulty) {
      const arrow = el("span", "arrow");
      arrow.append(document.createTextNode(slot.difficultyName), icon("i-chev"),
                   document.createTextNode(DIFFS[picked].name));
      hint.append(arrow);
    } else if (itemOnly) {
      hint.append(document.createTextNode("add the missing Soldier’s Earring"));
    } else {
      hint.append(document.createTextNode("current setting"));
    }
  }
  refresh();

  apply.onclick = async function () {
    const sentGrant = showGrant && grant.input.checked;
    const sentRevoke = showRevoke && revoke.input.checked;
    const opts = {
      path: slot.path,
      difficulty: picked,
      grantStartItems: sentGrant,
      revokeStartItems: sentRevoke,
    };
    const sameDifficulty = picked === slot.difficulty;
    apply.disabled = true;
    const label = apply.lastChild;
    label.textContent = "Checking…";
    out.innerHTML = "";
    try {
      const preview = await api("/api/swap", Object.assign({}, opts, { dryRun: true }));
      const ok = await ask({
        title: (sameDifficulty ? "Update " : "Change ") + slot.slot + "?",
        sub: "A timestamped backup is written before anything is touched.",
        // Nothing to show as a transition when only the inventory changes.
        from: sameDifficulty ? null : slot.difficulty, to: picked,
        lines: preview.changes,
      });
      if (!ok) { label.textContent = "Apply"; refresh(); return; }

      label.textContent = "Writing…";
      const res = await api("/api/swap", opts);
      out.append(el("pre", "log", res.changes.join("\n")));
      if (res.backup) {
        const done = el("div", "facts");
        done.append(chip("i-shield", "backup → " + res.backup, "mono path"));
        out.append(done);
      }
      // A switch is only ever offered when it would act, so if one was sent it
      // did act, and the save now holds the opposite inventory state.
      if (sentGrant) { slot.canGrantStartItem = false; slot.canRevokeStartItem = true; }
      if (sentRevoke) { slot.canGrantStartItem = true; slot.canRevokeStartItem = false; }

      slot.difficulty = picked;
      slot.difficultyName = DIFFS[picked].name;
      curIcon.firstChild.setAttribute("href", "#" + DIFFS[picked].sig);
      cur.lastChild.textContent = slot.difficultyName;
      cur.style.setProperty("--c", DIFFS[picked].hue);
      box.style.setProperty("--c", DIFFS[picked].hue);
      seg.repaint(picked);
      box.classList.add("done");
      setTimeout(function () { box.classList.remove("done"); }, 1000);
      toast(sameDifficulty
        ? slot.slot + " got its Soldier’s Earring"
        : slot.slot + " is now " + DIFFS[picked].name, "good");
    } catch (e) {
      out.append(el("div", "hint err", e.message));
      toast(e.message, "bad");
    }
    label.textContent = "Apply";
    refresh();
  };

  const act = el("div", "act");
  act.append(apply, hint);
  box.append(seg.node, extras, act, out, editorPanel(slot));
  return box;
}

/* -------------------------------------------------------------- details -- */
// Everything the format layer knows about a save: a summary to read, a form
// built out of the published schema to edit, and the document itself for the
// bulk edits a form is bad at. All three are the same JSON the dump and patch
// subcommands use, so none of them can validate differently from the CLI.
//
// Every value below reaches the DOM through textContent. Map paths and folder
// names come out of the save file, so they are attacker-controlled text.

// row renders one labelled line, or nothing when there is nothing to say.
function row(label, text) {
  if (!text) return null;
  const n = el("div", "drow");
  n.append(el("span", "dk", label), el("span", "dv", text));
  return n;
}

// named joins the "name" of each entry of a dump section, in index order.
function named(section, extra) {
  if (!section) return "";
  return Object.keys(section)
    .sort(function (a, b) { return Number(a) - Number(b); })
    .map(function (k) {
      const ent = section[k];
      return ent.name + (extra ? extra(ent) : "");
    })
    .join(", ");
}

// live drops the entries a save leaves at their empty value, so a fresh file
// does not render six lines of "Empty, Empty, Empty".
function live(section, isSet) {
  if (!section) return {};
  const out = {};
  for (const k of Object.keys(section)) if (isSet(section[k])) out[k] = section[k];
  return out;
}

function summary(doc) {
  const wrap = el("div", "detail");
  const h = doc.header || {};
  const recs = doc.records || {};

  const rows = [
    row("world", h.world_logo_name),
    row("location", h.location_name),
    row("map", h.map_path),
    row("spawn", h.map_spawn),
    row("save icon", h.save_icon_name),
    row("party", named(live(doc.party, function (e) { return e.id !== 0; }))),
    row("magic", named(live(doc.magic, function (e) { return e.id !== 0; }))),
    row("links", named(live(doc.links, function (e) { return e.id !== 0; }))),
    row("materials", named(doc.materials, function (e) { return " ×" + e.count; })),
    row("story", named(doc.story_flags, function (e) { return " " + e.value; })),
    row("attractions", named(live(recs.attractions, function (e) { return e.uses > 0; }),
      function (e) { return e.high_score ? " best " + e.high_score : ""; })),
    row("shotlocks", named(recs.shotlocks, function (e) { return " ×" + e.uses; })),
    row("crabs", h.crabs ? String(h.crabs) : ""),
    row("enemies defeated", h.enemies_defeated ? String(h.enemies_defeated) : ""),
  ];
  for (const r of rows) if (r) wrap.append(r);

  for (const name of Object.keys(doc.characters || {})) {
    const c = doc.characters[name];
    const worn = [];
    for (const kind of ["weapons", "armor", "accessories", "items"]) {
      const g = (c.equipment || {})[kind] || {};
      for (const s of Object.keys(g).sort()) if (g[s]) worn.push(g[s].name);
    }
    if (!worn.length) continue;
    const r = row(name, "hp " + c.hp + " mp " + c.mp + " — " + worn.join(", "));
    if (r) wrap.append(r);
  }
  return wrap;
}

// editorPanel is the whole detail view for one save: three tabs over one
// document, and one footer that validates and writes it.
function editorPanel(slot) {
  const wrap = el("div", "details");
  const toggle = pill("Open editor", "i-slider", "quiet");
  const body = el("div", "dbody");
  body.hidden = true;
  let built = false;

  toggle.onclick = async function () {
    if (built) {
      body.hidden = !body.hidden;
      toggle.lastChild.textContent = body.hidden ? "Open editor" : "Close editor";
      return;
    }
    toggle.disabled = true;
    toggle.lastChild.textContent = "Loading…";
    try {
      await loadSchema();
      const doc = await api("/api/detail?path=" + encodeURIComponent(slot.path));
      body.append(buildWorkbench(slot, doc));
      built = true;
      body.hidden = false;
      toggle.lastChild.textContent = "Close editor";
    } catch (e) {
      toast(e.message, "bad");
      toggle.lastChild.textContent = "Open editor";
    }
    toggle.disabled = false;
  };

  wrap.append(toggle, body);
  return wrap;
}

function clone(v) { return JSON.parse(JSON.stringify(v)); }

function buildWorkbench(slot, loaded) {
  const state = {
    doc: loaded,
    saved: clone(loaded),
    // A save that stops before the record block has no bests in its dump, and
    // the editor must say so rather than offering controls that fail on write.
    caps: { records: !!(loaded.records && loaded.records.minigames) },
    dirty: false,
  };

  const wrap = el("div", "work");
  const tabs = el("div", "tabs");
  const panes = el("div", "panes");
  const problemBar = el("div", "problems");
  const foot = el("div", "workfoot");

  let jsonView = null;
  let current = "overview";

  function rebuildOverview() {
    const pane = el("div", "pane");
    pane.append(summary(state.doc));
    return pane;
  }

  function rebuildForm() {
    const pane = el("div", "pane");
    pane.append(buildForm({
      doc: state.doc,
      caps: state.caps,
      edited: function () { state.dirty = true; check(); },
    }));
    return pane;
  }

  function rebuildJSON() {
    const pane = el("div", "pane");
    jsonView = jsonPane(state, check);
    pane.append(jsonView.node);
    return pane;
  }

  const builders = { overview: rebuildOverview, edit: rebuildForm, json: rebuildJSON };
  const tabButtons = {};

  function show(name) {
    // Leaving the JSON tab commits whatever is in the box, so an edit made
    // there is visible in the form and the summary. Text that does not parse
    // cannot be committed, and says so rather than being thrown away.
    if (current === "json" && jsonView && !jsonView.commit()) return;
    current = name;
    for (const k of Object.keys(tabButtons)) {
      tabButtons[k].setAttribute("aria-selected", k === name ? "true" : "false");
    }
    panes.innerHTML = "";
    if (name !== "json") jsonView = null;
    panes.append(builders[name]());
    check();
  }

  for (const tab of [["overview", "Summary", "i-search"],
                     ["edit", "Fields", "i-slider"],
                     ["json", "Document", "i-code"]]) {
    const b = pill(tab[1], tab[2], "tab");
    b.setAttribute("role", "tab");
    b.onclick = function () { show(tab[0]); };
    tabButtons[tab[0]] = b;
    tabs.append(b);
  }

  /* -- validation and writing -- */

  const preview = pill("Preview changes", "i-search", "quiet");
  const write = pill("Apply", "i-check", "go");
  const revert = pill("Revert", "i-refresh", "quiet");
  const log = el("div");

  function check() {
    const problems = validate(state.doc, state.caps);
    paintProblems(problemBar, problems, jsonView);
    if (jsonView) jsonView.markProblems(problems);
    const errs = problems.filter(function (p) { return p.severity !== "warn"; }).length;
    write.disabled = errs > 0;
    revert.disabled = !state.dirty;
    write.title = errs ? "fix the errors above first" : "";
    return problems;
  }

  async function send(dry) {
    log.innerHTML = "";
    try {
      const res = await api("/api/patch", { path: slot.path, doc: state.doc, dryRun: dry });
      if (!res.changes || !res.changes.length) {
        log.append(el("div", "hint", "Nothing would change."));
        return null;
      }
      log.append(el("pre", "log", res.changes.join("\n")));
      if (res.backup) {
        const done = el("div", "facts");
        done.append(chip("i-shield", "backup → " + res.backup, "mono path"));
        log.append(done);
        toast(slot.slot + " updated", "good");
        state.saved = clone(state.doc);
        state.dirty = false;
        check();
      }
      return res.changes;
    } catch (e) {
      log.append(el("div", "hint err", e.message));
      toast(e.message, "bad");
      return null;
    }
  }

  preview.onclick = function () { if (check() !== null) send(true); };
  write.onclick = async function () {
    const changes = await send(true);
    if (!changes) return;
    const ok = await ask({
      title: "Apply " + changes.length + " change" + (changes.length === 1 ? "" : "s") +
        " to " + slot.slot + "?",
      sub: "A timestamped backup is written before anything is touched.",
      from: null, to: null,
      lines: changes,
    });
    if (ok) send(false);
  };
  revert.onclick = function () {
    state.doc = clone(state.saved);
    state.dirty = false;
    panes.innerHTML = "";
    if (current === "json") jsonView = null;
    panes.append(builders[current]());
    check();
    toast("Back to what is on disk");
  };

  const acts = el("div", "act");
  acts.append(preview, write, revert);
  foot.append(el("p", "hint",
    "Keys left out are left alone. Everything here goes through the same checks " +
    "as the patch subcommand, and a timestamped backup is written before " +
    "anything is touched."), acts, log);

  wrap.append(tabs, problemBar, panes, foot);
  show("overview");
  return wrap;
}

// paintProblems is the validator's report. It mirrors what the server would
// say, so a mistake shows up while it is being typed rather than after a round
// trip -- but the server still has the last word, and nothing is written
// without its dry run agreeing.
function paintProblems(bar, problems, jsonView) {
  bar.innerHTML = "";
  if (!problems.length) {
    bar.className = "problems ok";
    bar.append(icon("i-check"), el("span", null, "The document checks out."));
    return;
  }
  const errs = problems.filter(function (p) { return p.severity !== "warn"; });
  bar.className = "problems " + (errs.length ? "bad" : "warn");
  const head = el("div", "phead");
  head.append(icon(errs.length ? "i-alert" : "i-warn"),
    el("span", null, errs.length
      ? errs.length + (errs.length === 1 ? " problem" : " problems") +
        (problems.length > errs.length ? ", " + (problems.length - errs.length) + " to look at" : "")
      : problems.length + (problems.length === 1 ? " thing" : " things") + " to look at"));
  bar.append(head);

  const list = el("div", "plist");
  for (const p of problems.slice(0, 40)) {
    const item = el("button", "pitem " + (p.severity === "warn" ? "warn" : "err"));
    item.type = "button";
    item.append(el("code", null, p.path || "document"), el("span", null, p.message));
    if (jsonView && p.line) item.onclick = function () { jsonView.goToLine(p.line); };
    else item.disabled = true;
    list.append(item);
  }
  if (problems.length > 40) list.append(el("p", "hint", (problems.length - 40) + " more"));
  bar.append(list);
}

/* ---------------------------------------------------------- json editor -- */

// jsonPane is the document view: a scope picker, the code editor, and the
// glue that keeps the text and the document in step. Scoping exists because a
// whole dump is a lot of text to hunt through when the thing being changed is
// one character's abilities, and Patch reads a partial document anyway.
function jsonPane(state, check) {
  const wrap = el("div", "jsonpane");

  const scopes = [{ key: "", label: "Whole document" }];
  for (const sec of SCHEMA.sections) {
    if (state.doc[sec.key] === undefined) continue;
    scopes.push({ key: sec.key, label: sec.label || sec.key });
    if (sec.shape !== "names") continue;
    for (const name of sec.keys) {
      if (state.doc[sec.key][name] === undefined) continue;
      scopes.push({ key: sec.key + "." + name, label: "  " + (sec.label || sec.key) + " · " + name });
    }
  }

  let scope = "";
  const picker = document.createElement("select");
  picker.className = "scope";
  picker.setAttribute("aria-label", "Which part of the document to edit");
  for (const s of scopes) {
    const opt = document.createElement("option");
    opt.value = s.key;
    opt.textContent = s.label;
    picker.append(opt);
  }

  function subtree() {
    if (!scope) return state.doc;
    const parts = scope.split(".");
    let at = state.doc;
    for (const p of parts) at = at[p];
    return at;
  }

  function putSubtree(v) {
    if (!scope) { state.doc = v; return; }
    const parts = scope.split(".");
    let at = state.doc;
    for (let i = 0; i < parts.length - 1; i++) at = at[parts[i]];
    at[parts[parts.length - 1]] = v;
  }

  let lines = new Map();
  let parseProblem = null;

  const code = codeEditor({
    value: JSON.stringify(subtree(), null, 2),
    onChange: function () { debounce(); },
  });

  const status = el("div", "jstatus");

  let timer = null;
  function debounce() {
    if (timer) clearTimeout(timer);
    timer = setTimeout(function () { timer = null; sync(); }, 140);
  }

  // sync parses the text and, when it parses, puts it back into the document
  // so the other two tabs and the validator see it. A document that does not
  // parse leaves the last good one alone: half-typed text is not an edit.
  function sync() {
    const text = code.get();
    try {
      const value = JSON.parse(text);
      parseProblem = null;
      putSubtree(value);
      state.dirty = true;
      lines = indexLines(text);
      status.className = "jstatus ok";
      status.textContent = text.length.toLocaleString() + " characters, " +
        text.split("\n").length.toLocaleString() + " lines";
      check();
    } catch (e) {
      parseProblem = parseError(text, e);
      lines = new Map();
      status.className = "jstatus bad";
      status.textContent = parseProblem.at
        ? "line " + parseProblem.at.line + ": " + parseProblem.message
        : parseProblem.message;
      code.mark(parseProblem.at ? [{ line: parseProblem.at.line, severity: "err" }] : []);
    }
  }

  picker.onchange = function () {
    if (!commit()) { picker.value = scope; return; }
    scope = picker.value;
    code.set(JSON.stringify(subtree(), null, 2));
    sync();
  };

  const tools = el("div", "jtools");
  const fmt = pill("Format", "i-code", "quiet tiny");
  fmt.onclick = function () {
    if (!commit()) return;
    code.set(JSON.stringify(subtree(), null, 2));
    sync();
  };
  const copy = pill("Copy", "i-copy", "quiet tiny");
  copy.onclick = function () { copyToClipboard(code.get(), "Document"); };
  tools.append(picker, fmt, copy);

  // commit is what the tab bar calls before leaving: it refuses to throw away
  // text that does not parse.
  function commit() {
    if (timer) { clearTimeout(timer); timer = null; }
    sync();
    if (!parseProblem) return true;
    toast("That is not valid JSON yet: " + parseProblem.message, "bad");
    return false;
  }

  sync();
  wrap.append(tools, code.node, status);
  return {
    node: wrap,
    commit: commit,
    goToLine: function (n) { code.goToLine(n); },
    // markProblems maps a validated path back to the line it sits on, which is
    // only possible while the text parses and only for the scope on screen.
    markProblems: function (problems) {
      if (parseProblem) return;
      const marks = [];
      for (const p of problems) {
        const rel = scope && p.path.indexOf(scope + ".") === 0
          ? p.path.slice(scope.length + 1) : (scope ? null : p.path);
        if (rel === null) continue;
        const line = lines.get(rel);
        if (!line) continue;
        p.line = line;
        marks.push({ line: line, severity: p.severity });
      }
      code.mark(marks);
    },
  };
}

/* -------------------------------------------------------------- folders -- */

function folderPanel(canBrowse, dirs) {
  const wrap = el("div");
  const list = el("div");
  for (const d of dirs) {
    const row = el("div", "folder");
    // Render the masked path: this row sits beside the masked account chip,
    // and on Steam the directory is named by the account id, so showing the
    // raw path here would undo the chip. Copy and the API still use d.path.
    row.append(icon(d.archive ? "i-archive" : "i-folder"), el("div", "p", d.displayPath || d.path));

    const tag = d.platform + (d.accountId ? " · " + d.accountId : "");
    if (d.archive) row.append(chip("i-archive", "zip", "hue archive"));
    row.append(chip(d.cloud ? "i-cloud" : null, tag));

    const acts = el("div", "acts");
    const copy = pill(null, "i-copy", "quiet round tiny");
    copy.title = "copy this path";
    copy.setAttribute("aria-label", "Copy path");
    copy.onclick = function () { copyToClipboard(d.path, "Path"); };
    acts.append(copy);

    if (d.platform === "added" || d.archive) {
      const drop = pill(null, "i-x", "quiet round tiny danger");
      drop.title = "forget this folder";
      drop.setAttribute("aria-label", "Forget this folder");
      drop.onclick = async function () {
        await api("/api/folder", { path: d.path, remove: true });
        toast("Folder forgotten");
        render();
      };
      acts.append(drop);
    }
    row.append(acts);
    list.append(row);
  }
  stagger(list);
  wrap.append(list);

  const controls = el("div", "controls");
  const field = el("div", "field");
  const input = document.createElement("input");
  input.type = "text";
  // One field for both kinds. Whether a path names a folder or a backup .zip
  // is worked out when it is added, so there is nothing to choose here.
  input.placeholder = "Paste a save folder or a backup .zip, then press Enter";
  input.setAttribute("aria-label", "Path to a save folder or a backup .zip");
  field.append(icon("i-search"), input);

  const msg = el("div", "hint");

  async function submit(path) {
    if (!path) return;
    msg.textContent = "";
    msg.className = "hint";
    try {
      await api("/api/folder", { path: path });
      toast("Folder added", "good");
      render();
    } catch (e) {
      msg.className = "hint err";
      msg.textContent = e.message;
    }
  }
  input.onkeydown = function (e) { if (e.key === "Enter") submit(input.value.trim()); };

  if (canBrowse) {
    const browse = pill("Browse…", "i-folder");
    browse.onclick = async function () {
      const label = browse.lastChild;
      browse.disabled = true;
      label.textContent = "Choosing…";
      try {
        const res = await api("/api/browse", {});
        if (!res.canceled) { render(); return; }
      } catch (e) {
        msg.className = "hint err";
        msg.textContent = e.message;
      }
      browse.disabled = false;
      label.textContent = "Browse…";
    };
    controls.append(browse);
  }
  controls.append(field);
  wrap.append(controls, msg);
  return wrap;
}

/* --------------------------------------------------------------- layout -- */

async function render() {
  let data;
  try {
    data = await api("/api/scan");
  } catch (e) {
    app.innerHTML = "";
    app.append(banner("hot", "i-alert", "Could not read saves", e.message));
    return;
  }

  app.innerHTML = "";

  if (data.gameRunning) {
    app.append(banner("hot", "i-play", "Kingdom Hearts III appears to be running",
      "Close it first. The game rewrites the whole save when it saves, so a change made now would be overwritten."));
  }
  if (data.dirs && data.dirs.some(function (d) { return d.archive; })) {
    app.append(banner("zip", "i-archive", "One of these is a backup archive",
      "Saves inside a .zip can be read and edited here, and the whole archive is backed up before it is rewritten. The game cannot read a zip, so unpack it back into your save folder before playing."));
  }
  if (data.dirs && data.dirs.some(function (d) { return d.cloud; })) {
    app.append(banner("warm", "i-cloud", "Steam Cloud is on for this save",
      "Turn Cloud off for the game in its Steam properties before applying, then back on once the change has loaded. Otherwise Steam can restore its own copy over the edit."));
  }

  if (!data.dirs || !data.dirs.length) {
    const blank = el("div", "empty");
    const art = el("div", "art");
    const img = document.createElement("img");
    img.src = EMBLEM;
    img.alt = "";
    const halo = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    halo.setAttribute("viewBox", "0 0 200 200");
    halo.setAttribute("aria-hidden", "true");
    const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
    use.setAttribute("href", "#rose");
    halo.append(use);
    art.append(img, halo);
    blank.append(art, el("h3", null, "No saves found yet"),
      el("p", null, "Checked Documents, OneDrive and the usual Proton prefixes. Point us at the folder once and it will be remembered."));
    app.append(blank, folderPanel(data.canBrowse, []));
    return;
  }

  app.append(heading("i-folder", "Save folders"));
  app.append(folderPanel(data.canBrowse, data.dirs));

  app.append(heading("i-stack", "Saves"));
  const saves = el("div");
  let n = 0;
  for (const d of data.dirs) for (const s of d.slots) saves.append(slotCard(s, n++));
  stagger(saves);
  app.append(saves);

  const again = pill("Rescan", "i-refresh", "quiet");
  again.onclick = function () { rescan(again); };
  const foot = el("div", "controls");
  foot.append(again);
  app.append(foot);
}

async function rescan(button) {
  // render() replaces the in-page button, but the one in the top bar survives,
  // so its icon has to be stopped explicitly or it spins for ever.
  const mark = button.querySelector("svg");
  if (mark) mark.classList.add("spin");
  try {
    await render();
    toast("Saves rescanned");
  } finally {
    if (mark) mark.classList.remove("spin");
  }
}

document.getElementById("topRescan").onclick = function () {
  rescan(this);
  scrollTo({ top: 0 });
};

// The masthead is tall on purpose; the condensed bar takes over once it goes.
addEventListener("scroll", function () {
  topbar.classList.toggle("on", scrollY > 170);
}, { passive: true });

render();
