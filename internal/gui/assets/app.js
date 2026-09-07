// kh3save UI: the shell. The library of save cards, the workspace that opens
// one, the three-tab editor over a single document, and the fragment route
// that names what is on screen.
//
// The rest of the page is beside this file: ui.js for the primitives, icons.js
// for the platform marks, editor.js for the code editor and json.js for the
// Document view built on it, schema.js and forms.js for the schema-driven
// Fields view, overview.js for the Summary dashboard, swap.js for the
// difficulty panel, folders.js for the folder list, legal.js for the notices.
// index.html names this file and only this file; everything else is reached
// through the imports below.
//
// Modules are always strict, so there is no "use strict" here. They are
// modules because the server hands every asset out under a path that carries
// the run's token, so a relative import inherits it; see assetPrefix in
// server.go for why the query string could not do that job.
import { diffMeta } from "./diffs.js";
import {
  EMBLEM,
  api,
  ask,
  banner,
  chip,
  el,
  heading,
  icon,
  pill,
  revealSpoilers,
  stagger,
  toast,
} from "./ui.js";
import { loadSchema, validate } from "./schema.js";
import { buildForm } from "./forms.js";
import { overview } from "./overview.js";
import { legalPage } from "./legal.js";
import { formatIcon } from "./icons.js";
import { difficultyPanel } from "./swap.js";
import { jsonPane, paintProblems } from "./json.js";
import { folderPanel } from "./folders.js";
const app = document.getElementById("app");
const topbar = document.getElementById("topbar");

// The cast is for the type checker, which cannot know from a selector
// string that these are images. Nothing about it changes at runtime.
const marks = /** @type {NodeListOf<HTMLImageElement>} */ (
  document.querySelectorAll("img.mark"));
for (const img of marks) img.src = EMBLEM;

/* ---------------------------------------------------- segmented control -- */


// Said in two places (once over a list where every folder has Cloud on, and
// once in the header of a save whose folder does), so it is written once.
const CLOUD_WARNING =
  "Turn Cloud off for the game in its Steam properties before applying, then " +
  "back on once the change has loaded. Otherwise Steam can restore its own " +
  "copy over the edit.";


/* ---------------------------------------------------------------- slots -- */
// A card is navigation. It carries enough to tell one save from another
// (who, where, how far), and every control that writes to the file lives in
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


/* ---------------------------------------------------------- json editor -- */

/* -------------------------------------------------------------- folders -- */


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
    // this state, which is exactly where the page should be saying what to
    // do next, not showing the emblem a third time.
    const blank = el("div", "empty");
    blank.append(el("h3", null, "No saves found yet"),
      el("p", null, "Checked Documents, OneDrive and the usual Proton prefixes. Point us at the folder once and it will be remembered."));
    app.append(blank, folderPanel(data.canBrowse, [], data.remembered || 0, render));
    return;
  }

  app.append(heading("i-folder", "Save folders"));
  app.append(folderPanel(data.canBrowse, data.dirs, data.remembered || 0, render));

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
