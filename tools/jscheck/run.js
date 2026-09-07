// Runs the page's own logic outside a browser: the validator against a dump
// that should be clean and a dozen documents that should not be, the line
// index against every key in the document, the highlighter against the text it
// must not alter, and the form builder against every section it can draw.
//
// Usage: node run.js <dir>, where <dir> holds schema.json and detail.json.
// internal/gui/js_test.go writes both and calls this.
"use strict";

const fs = require("fs");
const path = require("path");
const { pathToFileURL } = require("url");
const { doc } = require("./dom.js");

const dir = process.argv[2];
const assets = path.join(__dirname, "..", "..", "internal", "gui", "assets");
const schema = JSON.parse(fs.readFileSync(path.join(dir, "schema.json"), "utf8"));
const detail = JSON.parse(fs.readFileSync(path.join(dir, "detail.json"), "utf8"));

// The assets are ES modules, so they are imported rather than evaluated in a
// vm context: vm cannot run a module without an experimental flag, and there
// was never any isolation worth keeping here. This is a one-shot check, not a
// sandbox. The modules read these off the global scope the same way they would
// read them off a window.
// The fragment is the shell's router, so the stub has to be a real enough one
// to route: assigning to location.hash has to reach whatever the page
// registered for hashchange, or a card click sets a string and nothing else
// happens. Everything else stays inert.
const listeners = {};
const loc = { search: "?t=test", pathname: "/", hash: "" };
Object.defineProperty(loc, "hash", {
  get: function () { return loc._hash || ""; },
  set: function (v) {
    loc._hash = v && v[0] !== "#" ? "#" + v : v;
    for (const fn of listeners.hashchange || []) fn();
  },
});

// Object.assign throws on any global the runtime has made a getter-only
// accessor, and node 21 did exactly that to `navigator`: the checks passed on
// node 20 and failed on whatever the CI runner shipped. defineProperty
// replaces the descriptor outright, so a stub lands whatever is already there.
// The page only reads navigator.clipboard, and guards when it is absent.
function defineGlobals(globals) {
  for (const [name, value] of Object.entries(globals)) {
    Object.defineProperty(globalThis, name, {
      value: value, writable: true, configurable: true,
    });
  }
}

defineGlobals({
  document: doc,
  location: loc,
  navigator: {},
  Event: class { constructor(t) { this.type = t; } },
  // The shell reaches for these at import time.
  history: {
    pushState() {},
    back() {},
    // Dropping a stale fragment must not route again, which is the whole
    // reason the shell uses replaceState for it rather than assignment.
    replaceState() { loc._hash = ""; },
  },
  addEventListener(type, fn) { (listeners[type] = listeners[type] || []).push(fn); },
  scrollTo() {},
  scrollY: 0,
  // loadSchema is the only thing here that talks to the server, and it goes
  // through api(), which goes through fetch. Stubbing the transport rather
  // than the function is what keeps the real api() in the path being checked,
  // and answering by route is what lets the shell be driven at all.
  fetch: async function (url) {
    const at = String(url);
    const body = at.indexOf("/api/scan") > -1 ? SCAN
      : at.indexOf("/api/detail") > -1 ? detail
      : schema;
    return { ok: true, json: async function () { return body; } };
  },
});

// One folder holding the three shapes a slot comes in: a playthrough, the
// system file, and one that would not open. A card is navigation now, so the
// shell has to draw all three and open only the two that can be opened.
const SCAN = {
  canBrowse: true,
  gameRunning: false,
  dirs: [{
    path: "/saves", displayPath: "/saves", platform: "steam",
    accountId: "765611*******0000", cloud: true, archive: false,
    slots: [
      {
        file: "KHIII_slot0.bin", path: "/saves/KHIII_slot0.bin",
        displayPath: "/saves/KHIII_slot0.bin", slot: "slot0",
        difficulty: 3, difficultyName: "Critical", level: 7,
        playtime: "1:38:16", munny: 583, location: "Mount Olympus",
        account: "765611*******0000", format: "steam", world: "Olympus",
        cloud: true, canGrantStartItem: true, canRevokeStartItem: false,
      },
      {
        file: "KHIII_system.bin", path: "/saves/KHIII_system.bin",
        displayPath: "/saves/KHIII_system.bin", slot: "system",
        difficulty: 0, difficultyName: "", level: 0, playtime: "", munny: 0,
        location: "", account: "", format: "steam", world: "",
        cloud: true, canGrantStartItem: false, canRevokeStartItem: false,
      },
      {
        file: "bad.bin", path: "/saves/bad.bin", displayPath: "/saves/bad.bin",
        slot: "slot2", error: "could not decrypt this save",
      },
    ],
  }],
};

// The shell paints on import and again on navigation, both asynchronously.
// Nothing here fires a real event, so a couple of turns of the loop is what
// standing in for one looks like.
const settle = function () {
  return new Promise(function (r) { setTimeout(r, 0); });
};

// Namespace objects, not a merged copy: an export like SCHEMA is a live
// binding that loadSchema reassigns, and a snapshot taken now would read null
// for the rest of the run.
let mods = [];
const get = function (name) {
  for (const m of mods) if (name in m) return m[name];
  throw new Error("nothing exports " + name);
};
const load = function (file) {
  return import(pathToFileURL(path.join(assets, file)).href);
};

let fails = 0;
function ok(name, cond, extra) {
  if (cond) { console.log("  ok   " + name); return; }
  fails++;
  console.log("  FAIL " + name + (extra ? "\n         " + extra : ""));
}

(async function () {
  mods = await Promise.all(["diffs.js", "ui.js", "icons.js", "editor.js", "schema.js",
                            "forms.js", "overview.js", "legal.js", "swap.js",
                            "json.js", "folders.js"].map(load));
  await get("loadSchema")();
  const loadedSchema = get("SCHEMA");
  const validate = get("validate");
  const caps = { records: !!(detail.records && detail.records.minigames) };

  /* ------------------------------------------------------------ validator */

  ok("a dump of the fixture validates with nothing to report",
    validate(detail, caps).length === 0,
    JSON.stringify(validate(detail, caps).slice(0, 8)));

  const after = function (mutate) {
    const copy = JSON.parse(JSON.stringify(detail));
    mutate(copy);
    return validate(copy, caps);
  };
  const says = function (problems, re) {
    return problems.some(function (p) { return re.test(p.message); });
  };

  // Each of these is something Patch also refuses. The browser catching it
  // first is a convenience; the point is that it must never be the *only* one
  // to catch it, and must never be stricter than the server either.
  ok("a misspelled header key is caught",
    says(after(function (t) { t.header.munney = 1; }), /unknown key/));
  ok("a value past what the field can hold is caught",
    says(after(function (t) { t.header.level = 900; }), /above the maximum/));
  ok("a value the game will not show is only a warning",
    after(function (t) { t.header.level = 120; }).every(function (p) { return p.severity === "warn"; }));
  ok("an overlong map path is caught",
    says(after(function (t) { t.header.map_path = "x".repeat(400); }), /the field holds/));
  ok("an index outside the array is caught",
    says(after(function (t) { t.party["9"] = { id: 1 }; }), /outside 0-/));
  ok("a section that does not exist is caught",
    says(after(function (t) { t.gummiship = {}; }), /no section/));
  ok("a character that does not exist is caught",
    says(after(function (t) { t.characters.Xemnas = {}; }), /no character/));
  ok("an equipment entry with no type is caught",
    says(after(function (t) { t.characters.Sora.equipment.weapons["1"] = { id: 4 }; }), /needs a type/));
  ok("null clears an equipment slot and is not an error",
    after(function (t) { t.characters.Sora.equipment.weapons["0"] = null; }).length === 0);
  ok("an id no table covers is a warning, not an error",
    after(function (t) { t.magic["0"] = { id: 60000 }; }).every(function (p) { return p.severity === "warn"; }));
  ok("a fraction is caught",
    says(after(function (t) { t.header.munny = 1.5; }), /not a whole number/));

  if (caps.records) {
    const short = JSON.parse(JSON.stringify(detail));
    delete short.records.minigames;
    ok("a record field on a save that stops before the block is caught",
      says(validate(short, { records: false }), /record block/));
  }

  /* ----------------------------------------------------------- line index */

  const text = JSON.stringify(detail, null, 2);
  const lines = get("indexLines")(text);
  const rows = text.split("\n");
  const wrong = [];
  for (const entry of lines) {
    const key = entry[0].split(".").pop();
    const row = rows[entry[1] - 1];
    if (!row || row.indexOf(JSON.stringify(key) + ":") < 0) wrong.push(entry[0] + " -> " + entry[1]);
  }
  ok("every indexed path lands on the line its key is on (" + lines.size + " paths)",
    wrong.length === 0, wrong.slice(0, 5).join(", "));
  ok("a path nested three deep is indexed", lines.has("characters.Sora.hp"));
  ok("an entry of an indexed array is indexed",
    [...lines.keys()].some(function (k) { return /^inventory\.\d+\.count$/.test(k); }));

  /* ---------------------------------------------------------- highlighter */

  const html = get("highlightJSON")(text);
  const back = html.replace(/<[^>]+>/g, "")
    .replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&");
  ok("highlighting wraps the text and changes none of it", back === text);
  ok("highlighting tells keys from strings",
    /class="t-key"/.test(html) && /class="t-str"/.test(html));

  const broken = text.replace('"header": {', '"header": {{');
  try {
    JSON.parse(broken);
    ok("the broken document is actually broken", false);
  } catch (e) {
    const at = get("parseError")(broken, e).at;
    ok("a parse error reports a line", !!at && at.line >= 1);
  }

  /* ------------------------------------------------------- the form itself */

  let edits = 0;
  const state = {
    doc: JSON.parse(JSON.stringify(detail)), caps: caps,
    folds: new Map(), edited: function () { edits++; },
  };
  const form = get("buildForm")(state);
  const drawn = schema.sections.filter(function (s) { return state.doc[s.key] !== undefined; }).length;
  const heads = [...form.walk()].filter(function (n) { return n.classList.contains("fold-head"); });
  ok("one fold per section the save carries", heads.length === drawn,
    heads.length + " folds for " + drawn + " sections");

  // A section the schema marks as spoiling is covered in the form too, and the
  // form is where the whole party array and all sixteen character structs are
  // laid out, so this is the view where covering matters most.
  const spoiling = loadedSchema.sections.filter(function (sec) { return sec.spoils; });
  ok("the schema marks the regions that spoil", spoiling.length === 3,
    "marked " + spoiling.length);

  // Sections build their bodies on first open, so opening every one is the
  // only way to run the code that draws it. Opening a fold reveals more, and
  // a cover inside a fold only exists once that fold is open, so both are
  // driven in the same pass rather than one after the other.
  let opened = 0;
  let uncovered = 0;
  for (let pass = 0; pass < 10; pass++) {
    for (const n of [...form.walk()]) {
      if (n.done) continue;
      if (n.classList.contains("fold-head")) {
        n.done = true;
        n.onclick();
        opened++;
        continue;
      }
      if (n.classList.contains("pill") && n.textContent.indexOf("Show anyway") > -1) {
        n.done = true;
        n.onclick();
        uncovered++;
      }
    }
  }
  ok("the form covers every region the schema marks", uncovered === spoiling.length,
    "covered " + uncovered + " of " + spoiling.length);
  ok("every section body builds (" + opened + " folds)", opened > 40);

  const nodes = [...form.walk()];
  const has = function (c) { return nodes.some(function (n) { return n.classList.contains(c); }); };
  ok("number inputs were drawn", has("fin"));
  ok("pickers were drawn", has("combo"));
  ok("named bits were drawn", has("bit"));
  ok("sparse rows were drawn", has("srow"));
  ok("equipment slots were drawn", has("slot"));

  const numbers = nodes.filter(function (n) { return n.classList.contains("num"); });
  ok("a number input is there to drive", numbers.length > 0);
  if (numbers.length) {
    numbers[0].value = "4242";
    numbers[0].oninput();
    ok("editing through the form marks the document changed", edits > 0);
  }
  ok("the document still validates after the form has written to it",
    validate(state.doc, caps).length === 0,
    JSON.stringify(validate(state.doc, caps).slice(0, 5)));

  // Every tab switch rebuilds this form from the document, so the folds the
  // reader opened have to survive it. They live in state.folds for that
  // reason, and this is the check that says so.
  const openBefore = state.folds.size;
  ok("opening folds is recorded somewhere that outlives the form",
    openBefore > 40, "recorded " + openBefore);

  const again = get("buildForm")(state);
  const reopened = [...again.walk()].filter(function (n) {
    return n.classList.contains("fold") && n.classList.contains("open");
  }).length;
  ok("a rebuilt form comes back with those folds still open",
    reopened > 40, "reopened " + reopened);

  // And a fold that was deliberately closed stays closed, which is the half a
  // plain "remember what was opened" set would get wrong. The header is the
  // one fold that starts open, so it is the one that can prove it.
  state.folds.set("/header", false);
  const third = get("buildForm")(state);
  const header = [...third.walk()].filter(function (n) {
    return n.classList.contains("fold");
  })[0];
  ok("a fold that was closed on purpose stays closed",
    !!header && !header.classList.contains("open"));

  /* --------------------------------------------------------- the code box */

  const code = get("codeEditor")({ value: text });
  ok("the code editor builds", !!code.node);
  code.mark([{ line: 3, severity: "err" }, { line: 4, severity: "warn" }]);
  const gutter = [...code.node.walk()];
  ok("the gutter marks an error line",
    gutter.some(function (n) { return n.classList.contains("err"); }));
  ok("the gutter marks a warning line",
    gutter.some(function (n) { return n.classList.contains("warn"); }));

  /* --------------------------------------------------------- the dashboard */
  // The dashboard reads a dump directly rather than through the schema walker,
  // so a key it gets wrong renders nothing at all and no validator complains.
  // Building it against the same fixture the format tests use is what catches
  // that, and asserting on values rather than on node counts is what stops the
  // check passing on an empty grid.

  const board = get("overview")({ slot: "slot0", path: "/x", difficulty: 3 }, detail);
  ok("the dashboard builds", !!board);
  const seen = board.textContent;
  const shows = function (what) { return seen.indexOf(what) > -1; };

  for (const panel of ["Where", "Numbers", "Party", "Loadout", "Collection",
                       "Records", "Story"]) {
    ok("the dashboard draws the " + panel + " panel", shows(panel));
  }
  // A covered panel still draws: the heading and the schema note are what say
  // which region is under the cover, so neither may be hidden with it.

  // Values out of the fixture, so an empty panel cannot pass as a full one.
  ok("it reads the header", shows(detail.header.playtime) &&
    shows(String(detail.header.level)));

  // The two panels that say what is ahead of the player come up covered, and
  // the names they hold must genuinely not be in the document until asked
  // for: a cover that only hides them with CSS would still leak into a
  // screenshot, a find-in-page and a screen reader.
  const first = Object.keys(detail.characters)[0];
  const gear = detail.characters[first].equipment.weapons["0"].name;
  ok("the party is covered until it is asked for",
    !shows("HP " + detail.characters[first].hp) && !shows(gear),
    "a spoiler cover must not merely hide what it built");
  ok("the cover says what it is covering", shows("Hidden to avoid spoilers"));

  const reveal = [...board.walk()].filter(function (n) {
    return n.classList.contains("pill") && n.textContent.indexOf("Show anyway") > -1;
  });
  ok("both spoiler panels offer a way through", reveal.length === 2,
    "found " + reveal.length);
  for (const b of reveal) b.onclick();

  const open = board.textContent;
  const showsNow = function (what) { return open.indexOf(what) > -1; };
  ok("it draws a character sheet once revealed", showsNow(first) &&
    showsNow("HP " + detail.characters[first].hp));
  ok("it names equipped gear", showsNow(gear));
  ok("it counts what the save holds",
    shows(String(Object.keys(detail.materials).length)) || shows("held in total"));

  // The provenance notes are the schema's words, not this file's, so the
  // dashboard cannot claim more confidence than the format work does.
  const recSec = loadedSchema.sections.filter(function (sec) { return sec.key === "records"; })[0];
  ok("it carries the schema's note on the record block",
    !!recSec.note && shows(recSec.note));

  // The three header fields with a soft range are the only meters, and they
  // are drawn from the schema rather than from numbers typed into the page.
  const softs = loadedSchema.sections
    .filter(function (sec) { return sec.key === "header"; })[0].fields
    .filter(function (f) { return f.softMax; });
  ok("the schema still carries soft ranges to draw meters from", softs.length === 3);
  const meters = [...board.walk()].filter(function (n) { return n.classList.contains("meter"); });
  ok("a meter is drawn for level and for lucky emblems", meters.length === 2,
    "got " + meters.length);

  /* --------------------------------------------------------------- the shell */
  // app.js is the entry point, so importing it is what runs it: it paints the
  // list of saves on load. Nothing is exported for the sake of this check: a
  // card is opened by calling the handler the shell put on it, which is what a
  // click does, and that is the whole navigation path.

  await import(pathToFileURL(path.join(assets, "app.js")).href);
  await settle();
  await settle();

  const page = doc.getElementById("app");
  const reads = function () { return page.textContent; };
  const painted = function () { return [...page.walk()]; };

  ok("the shell paints the list of saves", reads().indexOf("slot0") > -1);
  ok("it draws every slot, including the one that will not open",
    reads().indexOf("system") > -1 && reads().indexOf("could not decrypt") > -1);

  // The swap moved into the workspace, so the card must no longer carry it.
  ok("the card is navigation and not a control surface",
    reads().indexOf("Apply") < 0 && reads().indexOf("current setting") < 0);

  const cards = painted().filter(function (n) {
    return n.classList.contains("nav") && typeof n.onclick === "function";
  });
  // Two of the three open: the broken one is inert on purpose.
  ok("two of the three slots are openable", cards.length === 2, "got " + cards.length);

  cards[0].onclick();
  await settle();
  await settle();

  const ws = reads();
  ok("opening a save reaches the workspace", ws.indexOf("All saves") > -1);
  // Read from the dump rather than from the scan stub: the head renders the
  // save that was opened, and hardcoding a value here would pass whatever the
  // fixture happened to hold.
  const named = get("diffMeta")(detail.header.difficulty).name;
  ok("the workspace leads with what the save is",
    ws.indexOf(named) > -1 && ws.indexOf("level " + detail.header.level) > -1,
    "wanted " + named + " and level " + detail.header.level);
  ok("the difficulty swap is one action in it, not the page",
    ws.indexOf("Change difficulty") > -1);
  ok("all three views are offered", ws.indexOf("Summary") > -1 &&
    ws.indexOf("Fields") > -1 && ws.indexOf("Document") > -1);
  ok("the dashboard is what it opens on", ws.indexOf("Olympus") > -1 &&
    ws.indexOf("Lucky emblems") > -1);

  ok("opening a save puts it in the fragment, so a reload finds it again",
    loc.hash === "#slot=0", "hash is " + JSON.stringify(loc.hash));

  /* --------------------------------------------------------------- notices */
  // Steam Cloud is a property of one folder, so the warning has to reach the
  // save it is true of rather than only the top of the list.
  ok("a save whose folder has Cloud on says so in its own header",
    reads().indexOf("Steam Cloud is on for slot0") > -1);

  // And every notice can be put away. Dismissing is remembered by title, so a
  // repaint does not bring it straight back.
  const dismiss = painted().filter(function (n) {
    return n.getAttribute("aria-label") === "Dismiss this notice";
  });
  ok("the notice offers a way to put it away", dismiss.length === 1,
    "found " + dismiss.length);
  dismiss[0].onclick();
  loc.hash = "";
  await settle();
  await settle();
  loc.hash = "slot=0";
  await settle();
  await settle();
  ok("and it stays away across a repaint",
    reads().indexOf("Steam Cloud is on for slot0") < 0);

  // An error banner is the whole content of a failed view, so it must not be
  // dismissable: putting it away would leave a blank page and no reason.
  ok("an error notice has no dismiss button",
    !get("banner")("hot", "i-alert", "Could not read saves", "why",
      { permanent: true }).walk().next().value.children.some(function (c) {
        return c.getAttribute && c.getAttribute("aria-label") === "Dismiss this notice";
      }));

  // The system file has no difficulty and no characters. It still opens, and
  // it has to say what it is rather than drawing an empty playthrough.
  cards[1].onclick();
  await settle();
  await settle();
  ok("the system file opens and says what it is",
    reads().indexOf("System file") > -1);

  /* ---------------------------------------------------------------- the legal page */
  // It is reachable without a scan on purpose, because it has to be readable
  // when nothing on this machine can be read at all, so it is worth checking
  // it builds and that the sentence a rights holder came for is in it.
  const notices = get("legalPage")();
  const said = notices.textContent;
  ok("the legal page builds", !!notices);
  for (const claim of ["KINGDOM HEARTS", "Simple Icons", "CC0 1.0",
                       "not affiliated with", "GPL-3.0"]) {
    ok("it states " + JSON.stringify(claim), said.indexOf(claim) > -1);
  }

  loc.hash = "legal";
  await settle();
  await settle();
  ok("and the fragment reaches it", reads().indexOf("Legal and attribution") > -1);
  ok("without needing a save to be readable first",
    reads().indexOf("KINGDOM HEARTS") > -1);

  /* ---------------------------------------------------------------- the route */
  // The fragment is what makes a save linkable and a reload survivable, so
  // arriving by fragment has to land in the same place a click does.

  loc.hash = "slot=0&tab=json";
  await settle();
  await settle();
  ok("a fragment can name the view to open on",
    reads().indexOf("All saves") > -1);

  loc.hash = "slot=0&tab=edit&open=header";
  await settle();
  await settle();
  const unfolded = [...page.walk()].filter(function (n) {
    return n.classList.contains("fold") && n.classList.contains("open");
  }).length;
  ok("and a section to unfold", unfolded > 0, "opened " + unfolded);

  // A link to a save that is not there must not leave the reader staring at a
  // blank page, and dropping the fragment must not route a second time.
  loc.hash = "slot=99";
  await settle();
  await settle();
  ok("a stale link falls back to the list", reads().indexOf("slot0") > -1);
  ok("and clears the fragment without routing again", loc.hash === "");

  // The broken slot is in the list but must not be reachable by index either.
  loc.hash = "slot=2";
  await settle();
  await settle();
  ok("a slot that would not open is not reachable by link",
    reads().indexOf("could not decrypt") > -1 && loc.hash === "");

  console.log(fails ? "\n" + fails + " check(s) failed" : "\nall good");
  process.exit(fails ? 1 : 0);
})().catch(function (e) {
  console.error("threw: " + (e && e.stack ? e.stack : e));
  process.exit(1);
});
