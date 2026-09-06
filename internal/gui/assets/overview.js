// The dashboard: what a save actually holds, read out of one dump.
//
// This is the view that answers "what is in here" without editing anything.
// It replaced a flat list of labelled rows, which could say that materials
// existed but not how many, and rendered a party of three as one line of
// comma-joined names.
//
// Two rules shape everything below, and both are the same rule this program
// applies to the format itself: say what is stored, and do not invent what is
// not. So there are no completion percentages, because nothing in the file
// carries a denominator for them -- with three exceptions, which are the three
// header fields the schema gives a soft range. Those get a meter, and it is
// drawn from schema.softMin/schema.softMax rather than from a number typed in
// here, so a change to the schema moves the meter and cannot leave it stale.
// Everything else is a count, and a count of what is there is honest.
//
// Every value reaches the DOM through textContent. Map paths, character names
// and folder names come out of the save file and are attacker-controlled text.
// Modules are always strict, so there is no "use strict" here. They are
// modules because the server hands every asset out under a path that carries
// the run's token, so a relative import inherits it; see assetPrefix in
// server.go for why the query string could not do that job.
import { chip, el, icon, pill } from "./ui.js";
import { SCHEMA } from "./schema.js";

/* ------------------------------------------------------------- schema -- */
// The dashboard reads the published schema for two things: the soft ranges
// that make a meter honest, and the notes that carry each region's provenance.
// Rendering the note rather than restating it is what keeps the page from
// claiming more confidence than the format work does.

function ovSection(key) {
  const list = (SCHEMA && SCHEMA.sections) || [];
  for (const sec of list) if (sec.key === key) return sec;
  return null;
}

function ovSub(parent, key) {
  const list = (parent && parent.sections) || [];
  for (const sec of list) if (sec.key === key) return sec;
  return null;
}

function ovHeaderField(key) {
  const sec = ovSection("header");
  for (const f of (sec && sec.fields) || []) if (f.key === key) return f;
  return null;
}

function ovNote(sec) {
  if (!sec || !sec.note) return null;
  const n = el("p", "ovnote");
  n.append(icon("i-warn"), el("span", null, sec.note));
  return n;
}

/* -------------------------------------------------------------- pieces -- */

function ovTileLabel(iconName, label) {
  const n = el("div", "tl");
  if (iconName) n.append(icon(iconName));
  n.append(el("span", null, label));
  return n;
}

function ovTile(iconName, label, value, sub) {
  const n = el("div", "tile");
  n.append(ovTileLabel(iconName, label), el("div", "tv", String(value)));
  if (sub) n.append(el("div", "ts", sub));
  return n;
}

// A meter is only ever drawn where the schema names a range the game actually
// respects. Anything outside that range still renders: a save edited past the
// soft cap is a real save, and clamping the bar would hide it.
function ovMeter(iconName, label, value, f) {
  const n = el("div", "tile meter");
  const lo = f.softMin || 0;
  const hi = f.softMax;
  const at = Math.max(0, Math.min(1, (value - lo) / (hi - lo || 1)));
  n.append(ovTileLabel(iconName, label), el("div", "tv", String(value)));
  const bar = el("div", "bar");
  const fill = el("i");
  fill.style.setProperty("width", (at * 100).toFixed(1) + "%");
  bar.append(fill);
  n.append(bar, el("div", "ts", "of " + hi));
  if (value > hi) {
    n.classList.add("over");
    n.append(el("div", "ts warn", f.softNote || "past what the game shows"));
  }
  return n;
}

function ovGrid(nodes) {
  const g = el("div", "tiles");
  for (const n of nodes) if (n) g.append(n);
  return g;
}

// A panel is one region of the save: a heading, an optional provenance note
// from the schema, and whatever the region turned out to hold. A region that
// holds nothing says so, because "empty" and "not mapped" are different
// answers and the difference matters here.
function ovPanel(title, iconName, body, note) {
  const box = el("section", "panel");
  const head = el("div", "panel-head");
  head.append(icon(iconName), el("h3", null, title));
  box.append(head);
  if (note) box.append(note);
  box.append(body);
  return box;
}

function ovEmpty(text) { return el("p", "ovempty", text); }

/* ------------------------------------------------------------ spoilers -- */
// Some of what a save holds says what is *ahead* of the player, not only what
// is behind them. A party sheet names the guest fighting beside Sora, and a
// list of worlds names worlds. Somebody opening a save to change its
// difficulty has not asked to be told who they are about to meet.
//
// So those panels come up covered, and the reader uncovers the ones they want.
// The choice is remembered for as long as the page is open, because having to
// re-confirm it on every tab switch would make it a nuisance rather than a
// courtesy, and it is deliberately not remembered any longer than that: a
// fresh run starts covered again.
const REVEALED = new Set();

// Uncover panels named in the fragment route. It is how a deep link into a
// save can say "and I already know what is in here", and how the screenshot
// script shoots the party sheets without a click it has no way to make.
export function revealSpoilers(keys) {
  for (const k of keys || []) if (k) REVEALED.add(k);
}

function ovSpoiler(key, warning, build) {
  const box = el("div", "spoiler");
  const body = el("div", "spoiler-body");

  const cover = el("div", "spoiler-cover");
  const badge = el("div", "spoiler-ico");
  badge.append(icon("i-eye-off"));
  const copy = el("div");
  copy.append(el("b", null, "Hidden to avoid spoilers"), el("p", null, warning));
  const show = pill("Show anyway", "i-eye", "quiet");
  cover.append(badge, copy, show);

  const hide = pill("Hide", "i-eye-off", "quiet tiny");

  let built = false;
  function reveal(on) {
    if (on && !built) { body.append(build()); built = true; }
    cover.hidden = on;
    body.hidden = !on;
    hide.hidden = !on;
    if (on) REVEALED.add(key); else REVEALED.delete(key);
  }
  show.onclick = function () { reveal(true); };
  hide.onclick = function () { reveal(false); };

  box.append(cover, hide, body);
  reveal(REVEALED.has(key));
  return box;
}

// Entries a save leaves at their empty value are dropped, so a fresh file does
// not render six copies of "Empty".
function ovLive(section, isSet) {
  const out = [];
  for (const key of Object.keys(section || {}).sort(function (a, b) { return Number(a) - Number(b); })) {
    const ent = section[key];
    if (isSet(ent)) out.push({ key: key, ent: ent });
  }
  return out;
}

// The left-hand label of a row in the detail list, with an optional mark.
function ovKey(iconName, label) {
  const n = el("span", "dk");
  if (iconName) n.append(icon(iconName));
  n.append(el("span", null, label));
  return n;
}

function ovChips(rows, label) {
  const wrap = el("div", "facts");
  for (const r of rows) wrap.append(chip(null, label(r)));
  return wrap;
}

/* ------------------------------------------------------------- where -- */

function ovWhere(doc) {
  const h = doc.header || {};
  const wrap = el("div", "detail");
  const rows = [
    ["i-globe", "world", h.world_logo_name],
    ["i-pin", "location", h.location_name],
    ["i-stack", "map", h.map_path],
    ["i-pin", "spawn", h.map_spawn],
    ["i-sparkle", "save icon", h.save_icon_name],
    ["i-users", "player", h.player_script],
    ["i-users", "character", h.player_character],
    ["i-clock", "written", h.saved_at],
  ];
  let any = false;
  for (const r of rows) {
    if (!r[2]) continue;
    any = true;
    const line = el("div", "drow");
    line.append(ovKey(r[0], r[1]), el("span", "dv", String(r[2])));
    wrap.append(line);
  }
  return any ? wrap : ovEmpty("This save has not reached a named map yet.");
}

/* ------------------------------------------------------------ numbers -- */

function ovNumbers(doc) {
  const h = doc.header || {};
  const tiles = [];

  const level = ovHeaderField("level");
  tiles.push(level && level.softMax
    ? ovMeter("i-level", "Level", h.level || 0, level)
    : ovTile("i-level", "Level", h.level || 0));

  tiles.push(ovTile("i-clock", "Playtime", h.playtime || "0:00:00"));

  // The ledger is three fields the format stores separately and Patch keeps
  // consistent, so showing the balance alone would hide the pair that has to
  // move with it.
  tiles.push(ovTile("i-coin", "Munny", (h.munny || 0).toLocaleString(),
    (h.munny_earned || h.munny_spent)
      ? (h.munny_earned || 0).toLocaleString() + " earned · " +
        (h.munny_spent || 0).toLocaleString() + " spent"
      : null));

  tiles.push(ovTile("i-sparkle", "Total EXP", (h.total_exp || 0).toLocaleString()));

  const crabs = ovHeaderField("crabs");
  tiles.push(crabs && crabs.softMax
    ? ovMeter("i-gem", "Lucky emblems", h.crabs || 0, crabs)
    : ovTile("i-gem", "Lucky emblems", h.crabs || 0));

  tiles.push(ovTile("i-sword", "Enemies defeated",
    (h.enemies_defeated || 0).toLocaleString()));
  tiles.push(ovTile("i-stack", "Times saved", h.saves_count || 0));

  const boosts = [];
  for (const b of [["HP", "bonus_hp"], ["MP", "bonus_mp"], ["Strength", "bonus_strength"],
                   ["Magic", "bonus_magic"], ["Defense", "bonus_defense"]]) {
    if (h[b[1]]) boosts.push(b[0] + " +" + h[b[1]]);
  }
  if (boosts.length) {
    tiles.push(ovTile("i-level", "Bonuses", boosts.length, boosts.join(" · ")));
  }

  return ovGrid(tiles);
}

/* -------------------------------------------------------------- party -- */
// One sheet per character the dump carries, which is Sora plus whoever the
// save has standing beside him. The four equipment arrays are shown by kind
// rather than joined into one list: an id means nothing without its type, and
// a sheet that blurred the four would be the interface making the same mistake
// the format layer takes care not to.

function ovAbilityCounts(ch) {
  let owned = 0, equipped = 0;
  const worn = [];
  for (const key of Object.keys(ch.abilities || {})) {
    const a = ch.abilities[key];
    if (a.owned) owned++;
    if (!a.equipped) continue;
    equipped++;
    worn.push(a.name);
  }
  return { owned: owned, equipped: equipped, worn: worn };
}

function ovKindRow(iconName, label, group) {
  const keys = Object.keys(group || {}).sort(function (a, b) { return Number(a) - Number(b); });
  const worn = [];
  for (const k of keys) if (group[k]) worn.push(group[k]);
  if (!worn.length) return null;
  const row = el("div", "drow");
  row.append(ovKey(iconName, label));
  const val = el("span", "dv facts");
  for (const e of worn) {
    // An entry that the game has switched off is still equipped, and saying so
    // is the difference between a slot that is empty and one that is muted.
    const c = chip(null, e.name + (e.enabled === false ? " (off)" : ""));
    if (e.enabled === false) c.classList.add("off");
    val.append(c);
  }
  row.append(val);
  return row;
}

function ovSheet(name, ch) {
  const box = el("div", "sheet");
  const head = el("div", "sheet-head");
  head.append(el("b", null, name));
  const vitals = el("div", "facts");
  vitals.append(chip("i-heart", "HP " + ch.hp), chip("i-sparkle", "MP " + ch.mp));
  if (ch.focus) vitals.append(chip(null, "Focus " + ch.focus));
  head.append(el("div", "grow"), vitals);
  box.append(head);

  const boosts = [];
  for (const b of [["ATK", "atk_boost"], ["MAG", "mag_boost"], ["DEF", "def_boost"], ["AP", "ap_boost"]]) {
    if (ch[b[1]]) boosts.push(b[0] + " +" + ch[b[1]]);
  }

  const body = el("div", "detail");
  const kinds = [["weapons", "i-sword"], ["armor", "i-shield"],
                 ["accessories", "i-ring"], ["items", "i-potion"]];
  for (const k of kinds) {
    const row = ovKindRow(k[1], k[0], (ch.equipment || {})[k[0]]);
    if (row) body.append(row);
  }

  const ab = ovAbilityCounts(ch);
  if (ab.owned) {
    const row = el("div", "drow");
    row.append(ovKey("i-sparkle", "abilities"),
      el("span", "dv", ab.equipped + " equipped of " + ab.owned + " owned"));
    body.append(row);
  }
  if (boosts.length) {
    const row = el("div", "drow");
    row.append(ovKey("i-level", "boosts"), el("span", "dv", boosts.join(" · ")));
    body.append(row);
  }
  if (ch.ai) {
    const bits = [];
    for (const k of ["combat_style", "ability_use", "recovery_use"]) {
      if (ch.ai[k] && ch.ai[k].name) bits.push(ch.ai[k].name);
    }
    if (bits.length) {
      const row = el("div", "drow");
      row.append(ovKey("i-users", "behavior"), el("span", "dv", bits.join(" · ")));
      body.append(row);
    }
  }
  box.append(body);
  return box;
}

function ovParty(doc) {
  const names = Object.keys(doc.characters || {});
  if (!names.length) return ovEmpty("No characters in this file.");
  const wrap = el("div", "sheets");
  for (const name of names) wrap.append(ovSheet(name, doc.characters[name]));

  // The party array is who stands beside Sora, and it is a different thing
  // from the characters the file carries stats for, so it is named separately
  // rather than folded into the sheets.
  const standing = ovLive(doc.party, function (e) { return e.id !== 0; });
  if (standing.length) {
    const line = el("div", "drow");
    line.append(ovKey("i-users", "party"),
      el("span", "dv", standing.map(function (r) { return r.ent.name; }).join(", ")));
    const holder = el("div", "detail");
    holder.append(line);
    wrap.append(holder);
  }
  return wrap;
}

/* ------------------------------------------------------------ loadout -- */

function ovLoadout(doc) {
  const wrap = el("div", "detail");

  const magic = ovLive(doc.magic, function (e) { return e.id !== 0; });
  const links = ovLive(doc.links, function (e) { return e.id !== 0; });

  const put = function (iconName, label, rows) {
    const line = el("div", "drow");
    line.append(ovKey(iconName, label));
    if (!rows.length) {
      line.append(el("span", "dv dimmed", "none"));
    } else {
      const val = el("span", "dv facts");
      for (const r of rows) val.append(chip(null, r.ent.name));
      line.append(val);
    }
    wrap.append(line);
  };
  put("i-wand", "magic", magic);
  put("i-link", "links", links);

  // Shortcuts read empty in every save this build was mapped from, and the
  // reason is in the schema note rather than in a guess made here: the game
  // does not persist the bindings it fills in from the magic list.
  let bound = 0;
  const pages = doc.shortcuts || {};
  for (const p of Object.keys(pages)) {
    for (const btn of Object.keys(pages[p] || {})) {
      const e = pages[p][btn];
      if (e && e.id) bound++;
    }
  }
  const line = el("div", "drow");
  line.append(ovKey("i-slider", "shortcuts"),
    el("span", "dv" + (bound ? "" : " dimmed"),
      bound ? bound + " bound across " + Object.keys(pages).length + " pages"
            : "none bound by hand"));
  wrap.append(line);
  return wrap;
}

/* --------------------------------------------------------- collection -- */

function ovTotals(section, key) {
  let distinct = 0, total = 0;
  for (const k of Object.keys(section || {})) {
    distinct++;
    total += Number(section[k][key]) || 0;
  }
  return { distinct: distinct, total: total };
}

function ovCollection(doc) {
  const inv = ovTotals(doc.inventory, "count");
  const mat = ovTotals(doc.materials, "count");
  const keys = ovLive(doc.keychain_upgrades, function (e) { return e.value !== 0; });

  const tiles = [
    ovTile("i-bag", "Items", inv.distinct, inv.total + " held in total"),
    ovTile("i-gem", "Materials", mat.distinct, mat.total + " held in total"),
    ovTile("i-sword", "Keychain upgrades", keys.length,
      keys.length ? null : "none recorded"),
  ];
  const wrap = el("div");
  wrap.append(ovGrid(tiles));

  // The long tails belong in the Fields tab. What earns space here is the
  // handful a save actually carries, which for materials is usually short.
  const mats = ovLive(doc.materials, function () { return true; });
  if (mats.length) {
    wrap.append(ovChips(mats, function (r) { return r.ent.name + " ×" + r.ent.count; }));
  }
  return wrap;
}

/* ------------------------------------------------------------ records -- */

function ovRecords(doc) {
  const recs = doc.records;
  if (!recs || !recs.minigames) {
    return ovEmpty("This file stops before the record block, so it holds no bests. " +
      "That is expected for a save shorter than a full slot.");
  }
  const wrap = el("div");

  const used = ovLive(recs.attractions, function (e) { return e.uses > 0; });
  const shots = ovLive(recs.shotlocks, function (e) { return e.uses > 0; });

  const rows = el("div", "detail");
  const put = function (iconName, label, list) {
    const line = el("div", "drow");
    line.append(ovKey(iconName, label));
    if (!list.length) {
      line.append(el("span", "dv dimmed", "never used"));
    } else {
      const val = el("span", "dv facts");
      for (const r of list) {
        val.append(chip(null, r.ent.name + " ×" + r.ent.uses +
          (r.ent.high_score ? "  best " + r.ent.high_score : "")));
      }
      line.append(val);
    }
    rows.append(line);
  };
  put("i-sparkle", "attractions", used);
  put("i-sword", "shotlocks", shots);
  wrap.append(rows);

  // Minigames and flans are upstream's names on offsets this build has never
  // seen carry a value, so they are shown only where one does, and the schema
  // note above the panel is what says why.
  const games = [];
  for (const k of Object.keys(recs.minigames || {})) {
    if (recs.minigames[k]) games.push(k.replace(/_/g, " ") + " " + recs.minigames[k]);
  }
  const flans = [];
  for (const k of Object.keys(recs.flans || {})) {
    const fl = recs.flans[k];
    if (fl && (fl.attempts || fl.high_score || fl.high_score_2)) {
      flans.push(k + " " + fl.high_score + " in " + fl.attempts);
    }
  }
  if (games.length || flans.length) {
    const extra = el("div", "facts");
    for (const g of games) extra.append(chip(null, g));
    for (const g of flans) extra.append(chip(null, g));
    wrap.append(extra);
  } else {
    wrap.append(ovEmpty("No minigame or flan record has been set in this save."));
  }
  if (recs.album && recs.album.photo_max_count) {
    wrap.append(el("p", "ovempty", "Photo album limit: " + recs.album.photo_max_count + "."));
  }
  return wrap;
}

/* -------------------------------------------------------------- story -- */
// Story flags are sparse, so every entry the dump carries is one the save has
// actually moved. The table names them "World" and "World - detail", so the
// part before the dash groups them; that is the table's own convention rather
// than a list of worlds typed in here, which is what keeps a regenerated
// table from leaving this stale.

function ovStory(doc) {
  const flags = doc.story_flags || {};
  const keys = Object.keys(flags);
  if (!keys.length) return ovEmpty("No story progress recorded yet.");

  const groups = new Map();
  for (const k of keys.sort(function (a, b) { return Number(a) - Number(b); })) {
    const ent = flags[k];
    const name = ent.name || ("flag " + k);
    const cut = name.indexOf(" - ");
    const head = cut > 0 ? name.slice(0, cut) : name;
    const tail = cut > 0 ? name.slice(cut + 3) : null;
    if (!groups.has(head)) groups.set(head, []);
    groups.get(head).push({ label: tail, value: ent.value });
  }

  const wrap = el("div", "worlds");
  for (const [head, list] of groups) {
    const card = el("div", "world");
    const top = el("div", "world-head");
    top.append(el("b", null, head));
    // The value counts how far that world got. There is no total to divide it
    // by anywhere in the file, so it is printed and not turned into a bar.
    const lead = list.find(function (x) { return x.label === null; });
    if (lead) top.append(el("span", "world-v", String(lead.value)));
    card.append(top);
    const parts = list.filter(function (x) { return x.label !== null; });
    if (parts.length) {
      const facts = el("div", "facts");
      for (const p of parts) facts.append(chip(null, p.label + " " + p.value));
      card.append(facts);
    }
    wrap.append(card);
  }
  return wrap;
}

/* --------------------------------------------------------------- view -- */

export function overview(slot, doc) {
  const wrap = el("div", "ov");
  const system = doc.characters && !Object.keys(doc.characters).length &&
    (!doc.header || doc.header.difficulty === undefined);

  wrap.append(ovPanel("Where", "i-globe", ovWhere(doc)));
  if (!system) wrap.append(ovPanel("Numbers", "i-level", ovNumbers(doc)));

  // The two that say what is ahead rather than what is behind. A guest party
  // member is somebody the player has met, but the sheet sits beside a name
  // they may not have, and the story list names worlds.
  wrap.append(ovPanel("Party", "i-users",
    ovSpoiler("party",
      "This names every character travelling with Sora in this save, including " +
      "the guest who joins in the world it was saved in.",
      function () { return ovParty(doc); })));

  wrap.append(ovPanel("Loadout", "i-wand", ovLoadout(doc),
    ovNote(ovSub(ovSection("shortcuts"), "shortcuts") || ovSection("shortcuts"))));
  wrap.append(ovPanel("Collection", "i-gem", ovCollection(doc)));
  wrap.append(ovPanel("Records", "i-trophy", ovRecords(doc), ovNote(ovSection("records"))));

  wrap.append(ovPanel("Story", "i-book",
    ovSpoiler("story",
      "This names the worlds this save has reached, and the beats it has " +
      "reached inside them.",
      function () { return ovStory(doc); }),
    ovNote(ovSection("story_flags"))));
  return wrap;
}
