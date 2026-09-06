// Shared primitives: the API client, the DOM helpers every view builds out of,
// the toast rail, the confirm dialog, and the combo box.
//
// No framework and no build step. These are plain ES modules the browser loads
// as written, and the binary embeds the same bytes.
// Modules are always strict, so there is no "use strict" here. They are
// modules because the server hands every asset out under a path that carries
// the run's token, so a relative import inherits it; see assetPrefix in
// server.go for why the query string could not do that job.
import { DIFFS } from "./diffs.js";

export const TOKEN = new URLSearchParams(location.search).get("t") || "";

// The token stays in the address bar deliberately. Scrubbing it with
// history.replaceState keeps it out of browser history, but then a reload has
// no token and the page dies on a bare 403 with no way back. The trade is not
// worth it: the token is 32 bytes of crypto/rand, minted per run and dead the
// moment the process exits, so a stale copy in history cannot be replayed.

// Where the server is handing out assets this run, token segment and all.
// Taking it from import.meta.url rather than rebuilding it out of TOKEN keeps
// the shape of an asset URL known in one place -- the server -- so a change
// there cannot leave a hand-assembled copy here pointing at a 403.
export const ASSETS = new URL(".", import.meta.url).href;

// An <img> cannot set the token header, so it takes the same token-carrying
// path the modules do. Same gate, same value, the transport the tag supports.
export const EMBLEM = ASSETS + "emblem.svg";

export async function api(path, body) {
  const r = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    headers: { "X-KH3-Token": TOKEN, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await r.json().catch(function () { return { error: r.statusText }; });
  if (!r.ok) throw new Error(data.error || r.statusText);
  return data;
}

export function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text != null) n.textContent = text;
  return n;
}

// <use> into the sprite in index.html, so an icon costs one small element.
export function icon(name) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", "#" + name);
  svg.append(use);
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  return svg;
}

export function chip(name, text, cls) {
  const n = el("span", "chip" + (cls ? " " + cls : ""));
  if (name) n.append(icon(name));
  n.append(el("span", null, text));
  return n;
}

export function pill(text, name, cls) {
  const b = el("button", "pill" + (cls ? " " + cls : ""));
  b.type = "button";
  if (name) b.append(icon(name));
  if (text) b.append(el("span", null, text));
  return b;
}

export function toast(text, kind) {
  const rail = document.getElementById("toasts");
  const n = el("div", "toast" + (kind ? " " + kind : ""));
  n.append(icon(kind === "bad" ? "i-alert" : kind === "good" ? "i-check" : "i-sparkle"),
    el("span", null, text));
  rail.append(n);
  setTimeout(function () {
    n.classList.add("out");
    setTimeout(function () { n.remove(); }, 320);
  }, 4600);
}

export function copyToClipboard(text, what) {
  if (!navigator.clipboard) { toast("Clipboard is not available here", "bad"); return; }
  navigator.clipboard.writeText(text).then(
    function () { toast((what || "Text") + " copied", "good"); },
    function () { toast("Could not copy", "bad"); });
}

// Stagger the entrance so a list cascades rather than snapping in.
export function stagger(parent) {
  parent.classList.add("stagger");
  Array.prototype.forEach.call(parent.children, function (node, i) {
    node.style.setProperty("--n", Math.min(i, 10));
  });
}

// The label wraps its own input, so these need no ids and can be built once
// per save rather than once per page.
export function optionSwitch(html, on) {
  const label = el("label", "opt");
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = !!on;
  const track = el("span", "track");
  const copy = el("span");
  // Hardcoded literals only. Everything that comes out of a save file reaches
  // the DOM through textContent, because folder names and map paths are
  // attacker-controlled text.
  copy.innerHTML = html;
  label.append(input, track, copy);
  return { node: label, input: input };
}

export function heading(name, text) {
  const h = el("h2");
  h.append(icon(name), el("span", null, text));
  return h;
}

// A banner, and every one of them can be put away.
//
// Dismissable is the default rather than something each call opts into: these
// say the same thing on every repaint, and a notice you have read, understood
// and cannot silence stops being a notice and becomes furniture. Opting out is
// for the ones where dismissing would leave the reader with nothing -- pass
// { permanent: true } and say why at the call site.
//
// The dismissal is remembered under the title, which is what makes it stick
// across a repaint, and it is forgotten when the page closes. That is
// deliberate: these describe live conditions -- the game is running, Steam
// Cloud is on -- and a dismissal that outlived the run would silence a warning
// that is still true on the next one, for a reader who has since forgotten it.
const DISMISSED = new Set();

export function banner(tone, name, title, text, opts) {
  const permanent = !!(opts && opts.permanent);
  // An already-dismissed banner answers with an empty fragment rather than
  // null: appending null puts the literal string "null" on the page, and the
  // caller should not have to remember that.
  if (!permanent && DISMISSED.has(title)) return document.createDocumentFragment();

  const n = el("div", "banner " + tone);
  const badge = el("div", "ico");
  badge.append(icon(name));
  const copy = el("div");
  copy.append(el("b", null, title), el("p", null, text));
  n.append(badge, copy);
  if (!permanent) {
    const close = pill(null, "i-x", "quiet round tiny");
    close.title = "dismiss";
    close.setAttribute("aria-label", "Dismiss this notice");
    close.onclick = function () { DISMISSED.add(title); n.remove(); };
    n.append(close);
  }
  return n;
}

/* ------------------------------------------------------------- spoilers -- */
// Some of what a save holds says what is *ahead* of the player, not only what
// is behind them: who travels with Sora, which worlds there are. Somebody who
// opened a save to change its difficulty has not asked to be told.
//
// So those regions come up covered and the reader uncovers the ones they want.
// Which regions those are is not decided here -- schema.spoils says, and the
// warning shown is the schema's own words -- because the dashboard and the
// schema-driven form both render them and a list kept in either one would go
// stale the moment a section moved.
//
// The cover does not hide a built subtree: build() is not called until the
// reader asks. That is the difference between hidden and absent, and it is
// what keeps the names out of a find-in-page, a screen reader and a
// screenshot rather than merely out of sight.
//
// REVEALED is module state, so a choice survives a tab switch and dies with
// the page. It is deliberately not persisted: a fresh run starts covered.
const REVEALED = new Set();

// Keys are schema section keys. A region is covered in both the dashboard and
// the form, and a link that says "party" means both, so the form's variant --
// keyed by path, because sections nest and two can share a key -- is named
// here rather than in every link.
export function revealSpoilers(keys) {
  for (const k of keys || []) {
    if (!k) continue;
    const bare = k.replace(/^\//, "");
    REVEALED.add(bare);
    REVEALED.add("form/" + bare);
  }
}

export function spoiler(key, warning, build) {
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

/* ---------------------------------------------------------------- modal -- */
// Replaces window.confirm(). The native dialog blocks the whole page and
// cannot show the change list with any structure; this can, and it inherits
// Esc, focus trapping and the backdrop for free from <dialog>.

export function ask(opts) {
  // <dialog>, for close() and showModal(). getElementById cannot say so
  // on its own, and this is the element the whole function is about.
  const modal = /** @type {HTMLDialogElement} */ (document.getElementById("modal"));
  return new Promise(function (resolve) {
    let settled = false;
    function finish(ok) {
      if (settled) return;
      settled = true;
      modal.close();
      resolve(ok);
    }

    document.getElementById("modalTitle").textContent = opts.title;
    document.getElementById("modalSub").textContent = opts.sub;

    const swap = document.getElementById("modalSwap");
    swap.innerHTML = "";
    if (opts.from != null) {
      const a = chip(DIFFS[opts.from].sig, DIFFS[opts.from].name, "hue");
      a.style.setProperty("--c", DIFFS[opts.from].hue);
      const arrow = icon("i-chev");
      arrow.setAttribute("class", "arrow-mark");
      const b = chip(DIFFS[opts.to].sig, DIFFS[opts.to].name, "hue");
      b.style.setProperty("--c", DIFFS[opts.to].hue);
      swap.append(a, arrow, b);
    }

    const body = document.getElementById("modalBody");
    body.innerHTML = "";
    body.append(el("pre", "log", opts.lines.length ? opts.lines.join("\n") : "no changes"));

    const yes = document.getElementById("modalYes");
    const no = document.getElementById("modalNo");
    yes.onclick = function () { finish(true); };
    no.onclick = function () { finish(false); };
    modal.onclose = function () { finish(false); };
    // Clicking the dimmed backdrop is the same as canceling.
    modal.onclick = function (e) { if (e.target === modal) finish(false); };

    modal.showModal();
    no.focus();
  });
}

/* ------------------------------------------------------------ combo box -- */
// A text field with a filtered list under it. This exists because the choices
// are the point: an id typed into a number box is how somebody equips a snack
// where they meant a keyblade. Every list here is a real game table, searchable
// by name, and the id is shown beside the name rather than instead of it.
//
// It is not a <datalist>: that cannot show the id alongside the name, cannot be
// styled, and on several browsers will not open on focus.

export function combo(opts) {
  // opts: { entries:[{id,name}], value, onPick(id), placeholder, allowFree }
  const wrap = el("div", "combo");
  const input = document.createElement("input");
  input.type = "text";
  input.autocomplete = "off";
  input.spellcheck = false;
  input.className = "combo-in";
  if (opts.placeholder) input.placeholder = opts.placeholder;

  const list = el("div", "combo-list");
  list.hidden = true;
  list.setAttribute("role", "listbox");

  let entries = opts.entries || [];
  let value = opts.value;
  let cursor = -1;
  let rows = [];

  function labelFor(id) {
    // A null value means nothing is chosen yet, which is what the "add one of
    // these" pickers start as: the box shows its placeholder rather than the
    // number -1 in the color reserved for an id no table covers.
    if (id === null || id === undefined) return "";
    for (const e of entries) if (e.id === id) return e.name + "  ·  " + id;
    return String(id);
  }

  function show() {
    input.value = labelFor(value);
    input.classList.toggle("unknown",
      value !== null && value !== undefined && !entries.some(function (e) { return e.id === value; }));
  }

  function close() { list.hidden = true; cursor = -1; }

  function paint(filter) {
    list.innerHTML = "";
    rows = [];
    const q = (filter || "").trim().toLowerCase();
    let shown = 0;
    for (const e of entries) {
      if (q && e.name.toLowerCase().indexOf(q) < 0 && String(e.id).indexOf(q) < 0) continue;
      if (shown >= 300) break; // the ability table is 512 rows; do not paint them all
      const row = el("div", "combo-row");
      row.setAttribute("role", "option");
      row.append(el("span", "cn", e.name), el("span", "ci", String(e.id)));
      if (e.id === value) row.classList.add("at");
      row.onmousedown = function (ev) { ev.preventDefault(); choose(e.id); };
      list.append(row);
      rows.push(row);
      shown++;
    }
    if (!shown) list.append(el("div", "combo-empty", "nothing matches"));
    list.hidden = false;
  }

  function choose(id) {
    value = id;
    show();
    close();
    if (opts.onPick) opts.onPick(id);
    // A picker that adds something keeps nothing selected, so the next one can
    // be typed straight away.
    if (opts.clearAfterPick) { value = null; show(); }
  }

  function move(step) {
    if (list.hidden) { paint(input.value); return; }
    if (!rows.length) return;
    if (cursor >= 0) rows[cursor].classList.remove("cur");
    cursor = (cursor + step + rows.length) % rows.length;
    rows[cursor].classList.add("cur");
    rows[cursor].scrollIntoView({ block: "nearest" });
  }

  input.onfocus = function () { input.select(); paint(""); };
  input.oninput = function () { cursor = -1; paint(input.value); };
  input.onblur = function () { close(); show(); };
  input.onkeydown = function (e) {
    if (e.key === "ArrowDown") { e.preventDefault(); move(1); return; }
    if (e.key === "ArrowUp") { e.preventDefault(); move(-1); return; }
    if (e.key === "Escape") { close(); show(); return; }
    if (e.key !== "Enter") return;
    e.preventDefault();
    if (cursor >= 0 && rows[cursor]) { rows[cursor].onmousedown(new Event("x")); return; }
    // A bare number is always accepted: the tables do not cover every id a
    // real save carries, and refusing one would make those saves uneditable.
    const n = Number(input.value.trim());
    if (Number.isInteger(n)) { choose(n); return; }
    const hit = entries.find(function (x) {
      return x.name.toLowerCase() === input.value.trim().toLowerCase();
    });
    if (hit) choose(hit.id); else show();
  };

  show();
  wrap.append(input, list);
  return {
    node: wrap,
    get: function () { return value; },
    set: function (v) { value = v; show(); },
    setEntries: function (e) { entries = e || []; show(); },
  };
}
