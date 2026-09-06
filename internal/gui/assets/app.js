// kh3save UI. No framework and no build step: the page is a few DOM calls
// against the local API described in server.go, split across four assets --
// ui.js for the primitives, editor.js for the JSON editor, schema.js and
// forms.js for the schema-driven half, and this file for the shell.
// Modules are always strict, so there is no "use strict" here. They are
// modules because the server hands every asset out under a path that carries
// the run's token, so a relative import inherits it; see assetPrefix in
// server.go for why the query string could not do that job.
import { CRITICAL, DIFFS, diffMeta } from "./diffs.js";
import {
  EMBLEM, api, ask, banner, chip, copyToClipboard, el, heading, icon,
  optionSwitch, pill, revealSpoilers, stagger, toast,
} from "./ui.js";
import { codeEditor, parseError } from "./editor.js";
import { SCHEMA, indexLines, loadSchema, validate } from "./schema.js";
import { buildForm } from "./forms.js";
import { overview } from "./overview.js";
import { legalPage } from "./legal.js";

const app = document.getElementById("app");
const topbar = document.getElementById("topbar");

// The cast is for the type checker, which cannot know from a selector
// string that these are images. Nothing about it changes at runtime.
const marks = /** @type {NodeListOf<HTMLImageElement>} */ (
  document.querySelectorAll("img.mark"));
for (const img of marks) img.src = EMBLEM;

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


// Said in two places -- once over a list where every folder has Cloud on, and
// once in the header of a save whose folder does -- so it is written once.
const CLOUD_WARNING =
  "Turn Cloud off for the game in its Steam properties before applying, then " +
  "back on once the change has loaded. Otherwise Steam can restore its own " +
  "copy over the edit.";

/* ------------------------------------------------------------ platforms -- */
// Which mark stands for a folder or a container. The value is a directory name
// off the disk, so it is matched loosely and falls back rather than leaving a
// row with a hole in it.
//
// The three brand marks are the only artwork in this program that is not
// original: they are Simple Icons' CC0 files, and they identify a platform
// rather than claim anything about it. See the sprite in index.html and the
// Legal page.
function platformIcon(name) {
  const at = String(name || "").toLowerCase();
  if (at.indexOf("steam") > -1) return "i-steam";
  if (at.indexOf("epic") > -1) return "i-epic";
  if (at.indexOf("zip") > -1) return "i-archive";
  if (at.indexOf("added") > -1) return "i-folder";
  return "i-stack";
}

// A container form, which is a different question from the platform: a save
// with no Steam wrapper is what a console writes, and this build has never
// been round-tripped through one, so the mark is as far as the claim goes.
function formatIcon(format) {
  return format === "steam" ? "i-steam" : "i-playstation";
}

/* ---------------------------------------------------------------- slots -- */
// A card is navigation. It carries enough to tell one save from another --
// who, where, how far -- and every control that writes to the file lives in
// the workspace it opens. The difficulty swap used to sit right here on the
// front page, which is what made a tool that maps the whole format read as a
// difficulty switcher with an editor bolted onto the side.

function slotCard(slot, index) {
  const box = el("div", "slot");
  const head = el("div", "slot-head");
  head.append(el("div", "idx", String(index + 1).padStart(2, "0")));

  // A slot that will not open is not navigation, so it stays inert and says
  // why rather than offering a workspace that cannot be built.
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

  // The system file has no difficulty and no characters, and Dump stops at
  // the difficulty byte for it. It still opens: the header it does carry is
  // editable, and a card that refused would be lying about that.
  const system = slot.slot === "system";
  const tone = system ? "var(--dim)" : diffMeta(slot.difficulty).hue;
  box.style.setProperty("--c", tone);

  const cur = el("div", "cur");
  cur.style.setProperty("--c", tone);
  cur.append(icon(system ? "i-slider" : diffMeta(slot.difficulty).sig),
             el("span", null, system ? "system" : slot.difficultyName));
  head.append(cur, el("div", "grow"));
  if (slot.path.indexOf(".zip!") > -1) head.append(chip("i-archive", "in zip", "hue archive"));
  head.append(chip(null, slot.slot, "mono"));
  const go = icon("i-chev");
  go.setAttribute("class", "slot-go");
  head.append(go);
  box.append(head);

  const facts = el("div", "facts");
  if (system) {
    facts.append(chip("i-stack", "shared config"), chip(null, "no difficulty"));
  } else {
    facts.append(
      chip("i-level", "level " + slot.level),
      chip("i-clock", slot.playtime),
      chip("i-coin", slot.munny + " munny"));
    if (slot.world) facts.append(chip("i-globe", slot.world));
    // A save that has not reached a named map yet would otherwise render an
    // empty pill with nothing but the pin in it.
    if (slot.location) facts.append(chip("i-pin", slot.location, "mono"));
  }
  box.append(facts);

  // The whole card is the target rather than a button inside it: the card is
  // one idea, and a hit area that covers only part of it reads as a bug.
  box.classList.add("nav");
  box.tabIndex = 0;
  box.setAttribute("role", "button");
  box.setAttribute("aria-label", "Open " + slot.slot);
  box.onclick = function () { openWorkspace(index); };
  box.onkeydown = function (e) {
    if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    openWorkspace(index);
  };
  return box;
}

/* --------------------------------------------------------- difficulty -- */
// The swap, lifted out of the card and into the workspace it belongs to. It
// is one action among many now rather than the whole front page, but it is
// still the only one that reasons about the save rather than about a field: a
// difficulty change moves HP, MP and three ability words as well as the flag,
// so a document already on screen is stale the moment it lands. The workspace
// reloads rather than trying to patch its own copy, and unsaved edits are
// called out before that happens rather than vanishing.

function difficultyPanel(slot, session, onSwapped) {
  const wrap = el("div", "dpanel");
  const apply = pill("Apply", "i-check", "go");
  const hint = el("span", "hint");
  const out = el("div");

  // The Soldier's Earring is the one thing Critical starts with that the
  // difficulty flag does not carry, so it is the only choice a swap has. Both
  // switches appear only when they would change something, and start in the
  // state that matches what the game would have done: Critical hands the
  // earring out, leaving Critical takes it back.
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
        // A swap writes the file, so whatever is unsaved in the editor is
        // about to be measured against a save it no longer describes. Saying
        // so here is cheaper than discovering it after the reload.
        sub: session.dirty()
          ? "A timestamped backup is written first. Unsaved edits in the editor will be dropped: the file changes underneath them."
          : "A timestamped backup is written before anything is touched.",
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
      seg.repaint(picked);
      toast(sameDifficulty
        ? slot.slot + " got its Soldier’s Earring"
        : slot.slot + " is now " + DIFFS[picked].name, "good");
      onSwapped();
      return;
    } catch (e) {
      out.append(el("div", "hint err", e.message));
      toast(e.message, "bad");
    }
    label.textContent = "Apply";
    refresh();
  };

  const act = el("div", "act");
  act.append(apply, hint);
  wrap.append(seg.node, extras, act, out);
  return wrap;
}
/* -------------------------------------------------------------- details -- */
// Everything the format layer knows about a save: a dashboard to read, a form
// built out of the published schema to edit, and the document itself for the
// bulk edits a form is bad at. All three are the same JSON the dump and patch
// subcommands use, so none of them can validate differently from the CLI. The
// dashboard is overview.js; the other two are below.
//
// Every value reaches the DOM through textContent. Map paths and folder names
// come out of the save file, so they are attacker-controlled text.

function clone(v) { return JSON.parse(JSON.stringify(v)); }

function buildWorkbench(slot, loaded, session, at) {
  const state = {
    doc: loaded,
    saved: clone(loaded),
    // A save that stops before the record block has no bests in its dump, and
    // the editor must say so rather than offering controls that fail on write.
    caps: { records: !!(loaded.records && loaded.records.minigames) },
    dirty: false,
    // Path -> open, remembered across the rebuilds a tab switch causes, and
    // seeded from the route so a link can open a save at a section.
    folds: new Map(),
  };
  for (const path of (at && at.open) || []) state.folds.set(path, true);
  for (const path of (at && at.shut) || []) state.folds.set(path, false);
  // The difficulty swap writes the file directly, so it has to know
  // whether there is unsaved work in here before it does.
  session.dirty = function () { return state.dirty; };

  const wrap = el("div", "work");
  const tabs = el("div", "tabs");
  const panes = el("div", "panes");
  const problemBar = el("div", "problems");
  const foot = el("div", "workfoot");

  let jsonView = null;
  let current = "overview";

  function rebuildOverview() {
    const pane = el("div", "pane");
    pane.append(overview(slot, state.doc));
    return pane;
  }

  function rebuildForm() {
    const pane = el("div", "pane");
    pane.append(buildForm({
      doc: state.doc,
      caps: state.caps,
      // Which folds are open belongs to the session, not to the form: the
      // form is rebuilt from the document every time this tab is shown.
      folds: state.folds,
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
  show(builders[(at && at.tab)] ? at.tab : "overview");
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

function folderPanel(canBrowse, dirs, remembered) {
  const wrap = el("div");
  const list = el("div");
  for (const d of dirs) {
    const row = el("div", "folder");
    // Render the masked path: this row sits beside the masked account chip,
    // and on Steam the directory is named by the account id, so showing the
    // raw path here would undo the chip. Copy and the API still use d.path.
    row.append(icon(d.archive ? "i-archive" : platformIcon(d.platform)),
      el("div", "p", d.displayPath || d.path));

    const tag = d.platform + (d.accountId ? " · " + d.accountId : "");
    if (d.archive) row.append(chip("i-archive", "zip", "hue archive"));
    row.append(chip(d.cloud ? "i-cloud" : platformIcon(d.platform), tag));

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

  // Folders could only be forgotten one at a time, and only the ones that were
  // added by hand: the autodetected rows carry no remove button, because they
  // come straight back on the next scan. So a list that has drifted -- a moved
  // folder, a backup zip that no longer exists, a path added while testing --
  // had no way back to a clean slate short of editing the config file. This is
  // that way back. It clears only what the store remembers; the autodetected
  // folders are found again immediately, and nothing near a save is touched.
  if (remembered > 0) {
    const reset = pill("Forget saved folders", "i-refresh", "quiet danger");
    reset.title = "Clear the folders this app has remembered";
    reset.onclick = async function () {
      const ok = await ask({
        title: "Forget saved folders?",
        sub: "This clears only the folders this app remembers. Your saves are not touched.",
        lines: [
          remembered === 1
            ? "1 remembered folder will be forgotten."
            : remembered + " remembered folders will be forgotten.",
          "",
          "Folders in the usual locations are detected again on the next scan.",
        ],
      });
      if (!ok) return;
      const res = await api("/api/folder", { clear: true });
      toast(res.cleared === 1 ? "1 folder forgotten" : res.cleared + " folders forgotten", "good");
      render();
    };
    controls.append(reset);
  }

  wrap.append(controls, msg);
  return wrap;
}

/* ------------------------------------------------------------ workspace -- */
// One save, opened. The list is navigation; this is where a file is read and
// written. Everything the format layer knows about is reachable from here
// through three views over one document, and the difficulty swap is one
// action in the header rather than the whole reason the page exists.

// The bar with the way out of a full-page view. Back rather than a link to the
// list, so it lands where the reader came from.
function backBar(label) {
  const bar = el("div", "wsbar");
  const back = pill(label, "i-chev", "quiet tiny back");
  back.onclick = function () { history.back(); };
  bar.append(back, el("div", "grow"));
  return bar;
}

function workspaceBar(slot) {
  const bar = backBar("All saves");
  if (slot.path.indexOf(".zip!") > -1) bar.append(chip("i-archive", "in zip", "hue archive"));
  bar.append(chip(null, slot.slot, "mono"));
  // A save with no Steam wrapper carries no account id, so the form is shown
  // instead of an empty chip. The id itself arrives already masked.
  if (slot.account) bar.append(chip("i-shield", slot.account, "mono"));
  if (slot.format) bar.append(chip(formatIcon(slot.format), slot.format));
  return bar;
}

function workspaceHead(slot, doc, session, reload) {
  const h = doc.header || {};
  const system = slot.slot === "system";
  const box = el("header", "wshead");
  const tone = system ? "var(--dim)" : diffMeta(h.difficulty).hue;
  box.style.setProperty("--c", tone);

  const title = el("div", "wstitle");
  const sig = icon(system ? "i-slider" : diffMeta(h.difficulty).sig);
  sig.setAttribute("class", "wssig");
  const names = el("div", "grow");
  names.append(el("h2", null, system ? "System file" : diffMeta(h.difficulty).name));
  const where = [h.world_logo_name, h.location_name].filter(Boolean).join("  ·  ");
  names.append(el("p", "sub", where || slot.displayPath));
  title.append(sig, names);
  box.append(title);

  if (system) {
    box.append(el("p", "hint",
      "Shared configuration, not a playthrough. It carries no difficulty, no " +
      "characters and no inventory, so the editor below shows only the header " +
      "fields it does hold."));
    return box;
  }

  const facts = el("div", "facts");
  facts.append(
    chip("i-level", "level " + h.level),
    chip("i-clock", h.playtime),
    chip("i-coin", h.munny + " munny"));
  if (h.saves_count) facts.append(chip("i-stack", "saved " + h.saves_count + "×"));
  box.append(facts);

  // The warning belongs where the writing happens, and it is only true of a
  // save whose own folder has Cloud on. Dismissable like any other, and the
  // title names the slot so putting it away for one save does not put it away
  // for the next.
  if (slot.cloud) {
    box.append(banner("warm", "i-cloud",
      "Steam Cloud is on for " + slot.slot, CLOUD_WARNING));
  }

  // A disclosure rather than a permanent panel: the swap is the one action
  // here that rewrites the file on its own terms, and leaving it open would
  // put it back in the position this redesign took it out of.
  const toggle = pill("Change difficulty", "i-slider", "quiet");
  const panel = el("div");
  panel.hidden = true;
  panel.append(difficultyPanel(slot, session, reload));
  toggle.onclick = function () {
    panel.hidden = !panel.hidden;
    toggle.lastChild.textContent = panel.hidden ? "Change difficulty" : "Hide difficulty";
  };
  const acts = el("div", "controls");
  acts.append(toggle);
  box.append(acts, panel);
  return box;
}

async function paintWorkspace(slot, at) {
  const holder = el("div");
  holder.append(el("div", "skel"), el("div", "skel"));
  app.append(workspaceBar(slot), holder);

  let doc;
  try {
    await loadSchema();
    doc = await api("/api/detail?path=" + encodeURIComponent(slot.path));
  } catch (e) {
    holder.innerHTML = "";
    holder.append(banner("hot", "i-alert", "Could not open this save", e.message,
      { permanent: true }));
    return;
  }

  // The workbench owns the document; the head only needs to know whether it
  // has unsaved work in it, which is what session carries between them.
  if (at && at.reveal) revealSpoilers(at.reveal);

  const session = { dirty: function () { return false; } };
  const bench = buildWorkbench(slot, doc, session, at);
  const head = workspaceHead(slot, doc, session, function () {
    // The swap wrote the file. Rebuild from disk rather than reasoning about
    // what moved: it touches HP, MP and three ability words as well as the
    // flag, and a card in the list behind us now reads the old difficulty.
    SCAN = null;
    app.innerHTML = "";
    paintWorkspace(slot, at);
  });

  holder.innerHTML = "";
  holder.append(head, bench);
}

/* --------------------------------------------------------------- layout -- */
// The fragment is the route.
//
// It replaced a pushState that pushed the same URL, which gave Back something
// to pop and nothing else: a reload landed back on the list, having thrown
// away which save was open. A fragment survives the reload, leaves the token
// in the query untouched, and makes a save's editor something that can be
// linked to at all.
//
//   #slot=2                         the third save in the list
//   #slot=2&tab=edit                opened on Fields
//   #slot=2&tab=edit&open=header    with a section already unfolded
//   #slot=2&tab=edit&shut=header    or with the one it opens by default shut
//   #slot=2&reveal=party,story_flags   and the spoiler covers already lifted
//   #legal                          the notices page
//
// The index is into the list as it is drawn, which is the only stable name a
// slot has here: a path would put a save folder in the address bar, and on
// Steam that names the account.

const wrapNode = document.querySelector(".wrap");

// The last scan, kept so stepping back out of a save is instant. Anything
// that writes a file drops it, because a card renders what is on disk.
let SCAN = null;

function readRoute() {
  const at = { open: [], shut: [], reveal: [] };
  for (const part of location.hash.replace(/^#/, "").split("&")) {
    if (!part) continue;
    const cut = part.indexOf("=");
    const key = decodeURIComponent(cut < 0 ? part : part.slice(0, cut));
    const val = cut < 0 ? "" : decodeURIComponent(part.slice(cut + 1));
    if (key === "open" || key === "shut" || key === "reveal") {
      at[key] = val.split(",").filter(Boolean).map(function (v) {
        // Fold paths are rooted, so a link may leave the leading slash off.
        return key !== "reveal" && v[0] !== "/" ? "/" + v : v;
      });
      continue;
    }
    at[key] = val;
  }
  return at;
}

// Every slot in the order the cards are drawn, which is what an index means.
function allSlots() {
  const out = [];
  for (const d of (SCAN && SCAN.dirs) || []) for (const sl of d.slots) out.push(sl);
  return out;
}

function openWorkspace(index) { location.hash = "slot=" + index; }

async function route() {
  app.innerHTML = "";
  scrollTo({ top: 0 });

  const at = readRoute();

  // The notices page needs no scan behind it, so it is answered before one is
  // fetched: it has to be readable even when nothing on this machine can be
  // read at all.
  if (at.legal !== undefined) {
    // Prose, so it keeps the narrow measure the list uses rather than the
    // dashboard's width, which would leave it as a column against dead space.
    wrapNode.classList.remove("wide");
    app.append(backBar("All saves"), legalPage());
    return;
  }

  const index = Number(at.slot);
  const wanted = at.slot !== undefined && Number.isInteger(index) && index >= 0;
  // The list reads well narrow. One save's dashboard does not.
  wrapNode.classList.toggle("wide", wanted);

  if (!SCAN) {
    app.append(el("div", "skel"), el("div", "skel"));
    try {
      SCAN = await api("/api/scan");
    } catch (e) {
      app.innerHTML = "";
      app.append(banner("hot", "i-alert", "Could not read saves", e.message,
        { permanent: true }));
      return;
    }
    app.innerHTML = "";
  }

  const slot = wanted ? allSlots()[index] : null;
  if (wanted && (!slot || slot.error)) {
    // A stale link, or one to a save that has since gone. Drop the fragment
    // with replaceState rather than by assignment, which would fire hashchange
    // and route again.
    history.replaceState(null, "", location.pathname + location.search);
    wrapNode.classList.remove("wide");
    return paintLibrary();
  }
  if (slot) return paintWorkspace(slot, at);
  return paintLibrary();
}

addEventListener("hashchange", function () { route(); });

// render refetches and shows whatever the route asks for. folderPanel calls it
// once a folder is added or forgotten, and both Rescan buttons go through it.
async function render() {
  SCAN = null;
  await route();
}

function paintLibrary() {
  const data = SCAN;

  if (data.gameRunning) {
    app.append(banner("hot", "i-play", "Kingdom Hearts III appears to be running",
      "Close it first. The game rewrites the whole save when it saves, so a change made now would be overwritten."));
  }
  if (data.dirs && data.dirs.some(function (d) { return d.archive; })) {
    app.append(banner("zip", "i-archive", "One of these is a backup archive",
      "Saves inside a .zip can be read and edited here, and the whole archive is backed up before it is rewritten. The game cannot read a zip, so unpack it back into your save folder before playing."));
  }
  // Steam Cloud is a property of one folder, not of the machine. When every
  // folder has it the warning is about all of them and belongs at the top;
  // when only some do, saying "this save" above a list containing saves it is
  // not true of is worse than not saying it, so it moves onto the cards.
  const clouded = (data.dirs || []).filter(function (d) { return d.cloud; });
  if (clouded.length && clouded.length === (data.dirs || []).length) {
    app.append(banner("warm", "i-cloud", "Steam Cloud is on for these saves", CLOUD_WARNING));
  }

  if (!data.dirs || !data.dirs.length) {
    // No third copy of the mark here. The sigil in the masthead and the rose
    // turning behind it are already on screen, and a fresh install lands on
    // this state -- which is exactly where the page should be saying what to
    // do next, not showing the emblem a third time.
    const blank = el("div", "empty");
    blank.append(el("h3", null, "No saves found yet"),
      el("p", null, "Checked Documents, OneDrive and the usual Proton prefixes. Point us at the folder once and it will be remembered."));
    app.append(blank, folderPanel(data.canBrowse, [], data.remembered || 0));
    return;
  }

  app.append(heading("i-folder", "Save folders"));
  app.append(folderPanel(data.canBrowse, data.dirs, data.remembered || 0));

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
